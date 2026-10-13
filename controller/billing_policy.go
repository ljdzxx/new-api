package controller

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/gin-gonic/gin"
)

var billingPolicyWriteMutex sync.Mutex

type billingPolicyTransitionRequest struct {
	SourceChecksum string `json:"source_checksum"`
}

type billingPolicyModelPolicyRequest struct {
	Model  string                `json:"model"`
	Policy billing_policy.Policy `json:"policy"`
}

func PreviewBillingPolicyMigration(c *gin.Context) {
	report, err := billing_policy.BuildLegacyMigrationCandidate(billing_policy.StateShadow)
	c.JSON(http.StatusOK, gin.H{"success": err == nil, "message": errorMessage(err), "data": report})
}

func GetBillingPolicyState(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"config": billing_policy.GetConfig(), "shadow": billing_policy.GetShadowStats()}})
}

func UpdateBillingPolicyConfig(c *gin.Context) {
	billingPolicyWriteMutex.Lock()
	defer billingPolicyWriteMutex.Unlock()
	var next billing_policy.Config
	if err := common.DecodeJson(c.Request.Body, &next); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	current := billing_policy.GetConfig()
	if current.State != billing_policy.StateActive {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "billing policy editing is available only after activation"})
		return
	}
	if next.Revision != current.Revision {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "billing policy revision conflict", "data": current})
		return
	}
	next.State = billing_policy.StateActive
	next.SchemaVersion = billing_policy.SchemaVersion
	next.Migration = current.Migration
	next.Revision++
	if err := persistBillingPolicyConfig(next); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": next})
}

func UpsertBillingPolicyModelPolicy(c *gin.Context) {
	billingPolicyWriteMutex.Lock()
	defer billingPolicyWriteMutex.Unlock()
	var req billingPolicyModelPolicyRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model name is required"})
		return
	}
	if err := billing_policy.ValidatePolicy(req.Policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	current := billing_policy.GetConfig()
	// A brand-new installation has no migrated policies or migration metadata.
	// Let its first policy write bootstrap the new engine directly; requiring a
	// shadow migration here makes the initial pricing setup impossible without
	// an unrelated migration workflow. Existing legacy configurations still
	// remain protected and must go through the migration process.
	if current.State == billing_policy.StateLegacy &&
		current.Migration.Version == 0 && len(current.Policies) == 0 {
		current.State = billing_policy.StateActive
	}
	if current.State != billing_policy.StateActive {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "billing policy editing is available only after activation"})
		return
	}
	if current.Policies == nil {
		current.Policies = map[string]billing_policy.Policy{}
	}
	current.Policies[modelName] = req.Policy
	current.SchemaVersion = billing_policy.SchemaVersion
	current.State = billing_policy.StateActive
	current.Revision++
	if err := persistBillingPolicyConfig(current); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": current})
}

// StartBillingPolicyShadow snapshots all legacy prices and enables comparison-only
// evaluation. The legacy engine remains authoritative until ActivateBillingPolicy.
func StartBillingPolicyShadow(c *gin.Context) {
	billingPolicyWriteMutex.Lock()
	defer billingPolicyWriteMutex.Unlock()
	report, err := billing_policy.BuildLegacyMigrationCandidate(billing_policy.StateShadow)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error(), "data": report})
		return
	}
	report.Candidate.Revision = billing_policy.GetConfig().Revision + 1
	if err = persistBillingPolicyConfig(report.Candidate); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	billing_policy.ResetShadowStats()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}

// PrepareBillingPolicySwitch freezes legacy price writes after rechecking that
// the source configuration is identical to the snapshot used by shadow mode.
func PrepareBillingPolicySwitch(c *gin.Context) {
	billingPolicyWriteMutex.Lock()
	defer billingPolicyWriteMutex.Unlock()
	var req billingPolicyTransitionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	config := billing_policy.GetConfig()
	if config.State != billing_policy.StateShadow {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "billing policy must be in shadow state"})
		return
	}
	stats := billing_policy.GetShadowStats()
	if !billing_policy.ShadowReadyForSwitch(stats) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "shadow verification requires settlement observations with no settlement mismatches or calculation errors", "data": stats})
		return
	}
	checksum := billing_policy.SourceChecksum(billing_policy.LegacySourceValues())
	if req.SourceChecksum == "" || req.SourceChecksum != config.Migration.SourceChecksum || checksum != config.Migration.SourceChecksum {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "legacy pricing changed after shadow migration; rerun shadow migration"})
		return
	}
	config.State = billing_policy.StatePrepared
	config.Revision++
	if err := persistBillingPolicyConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": config})
}

// ActivateBillingPolicy performs the maintenance-window cutover. Because the
// engine state and the complete policy set live in one option value, readers
// observe either the prepared legacy state or the complete active state.
func ActivateBillingPolicy(c *gin.Context) {
	billingPolicyWriteMutex.Lock()
	defer billingPolicyWriteMutex.Unlock()
	var req billingPolicyTransitionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	config := billing_policy.GetConfig()
	if config.State != billing_policy.StatePrepared {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "billing policy must be prepared before activation"})
		return
	}
	checksum := billing_policy.SourceChecksum(billing_policy.LegacySourceValues())
	if req.SourceChecksum == "" || checksum != req.SourceChecksum || checksum != config.Migration.SourceChecksum {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "legacy pricing checksum changed; activation aborted"})
		return
	}
	config.State = billing_policy.StateActive
	config.Revision++
	if err := persistBillingPolicyConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": config})
}

func CancelBillingPolicyMigration(c *gin.Context) {
	billingPolicyWriteMutex.Lock()
	defer billingPolicyWriteMutex.Unlock()
	config := billing_policy.GetConfig()
	if config.State == billing_policy.StateActive {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "active policies cannot be rolled back automatically after new-policy edits"})
		return
	}
	config.State = billing_policy.StateLegacy
	config.Revision++
	if err := persistBillingPolicyConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	billing_policy.ResetShadowStats()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": config})
}

func persistBillingPolicyConfig(config billing_policy.Config) error {
	if err := billing_policy.ValidateConfig(&config); err != nil {
		return err
	}
	data, err := common.Marshal(config)
	if err != nil {
		return err
	}
	if err := model.UpdateBillingPolicyOptionChecked(string(data), config.Revision-1); err != nil {
		return err
	}
	model.InvalidatePricingCache()
	return nil
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// PreviewLegacyModelPrices returns availability and effective policy coverage
// using backend matching rules, so the UI never guesses billing behavior.
func PreviewLegacyModelPrices(c *gin.Context) {
	var req struct {
		Models []string `json:"models"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if len(req.Models) > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "too many models"})
		return
	}
	config := billing_policy.GetConfig()
	entries := map[string]gin.H{}
	for _, name := range req.Models {
		_, configured := billing_policy.ResolveConfig(config, name)
		entry := gin.H{"configured": configured, "available": false}
		if !configured {
			_, err := billing_policy.BuildLegacyModelPolicy(name)
			entry["available"] = err == nil
			if err != nil {
				entry["reason"] = err.Error()
			}
		}
		entries[name] = entry
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"config": config, "models": entries}})
}

func SyncLegacyModelPrice(c *gin.Context) {
	billingPolicyWriteMutex.Lock()
	defer billingPolicyWriteMutex.Unlock()
	var req struct {
		Model    string `json:"model"`
		Revision int64  `json:"revision"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Model)
	if name == "" || strings.Contains(name, "*") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "a concrete model name is required"})
		return
	}
	config := billing_policy.GetConfig()
	if config.State != billing_policy.StateActive || config.Revision != req.Revision {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "billing policy must be active and revision must match; refresh and retry"})
		return
	}
	if _, ok := billing_policy.ResolveConfig(config, name); ok {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "model already has an effective billing policy"})
		return
	}
	policy, err := billing_policy.BuildLegacyModelPolicy(name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	config.Policies[name] = policy
	config.Revision++
	if err = persistBillingPolicyConfig(config); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, model.ErrBillingPolicyRevisionConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": config})
}
