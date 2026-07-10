package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/gin-gonic/gin"
)

type billingPolicyTransitionRequest struct {
	SourceChecksum string `json:"source_checksum"`
}

func PreviewBillingPolicyMigration(c *gin.Context) {
	report, err := billing_policy.BuildLegacyMigrationCandidate(billing_policy.StateShadow)
	c.JSON(http.StatusOK, gin.H{"success": err == nil, "message": errorMessage(err), "data": report})
}

func GetBillingPolicyState(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"config": billing_policy.GetConfig(), "shadow": billing_policy.GetShadowStats()}})
}

// StartBillingPolicyShadow snapshots all legacy prices and enables comparison-only
// evaluation. The legacy engine remains authoritative until ActivateBillingPolicy.
func StartBillingPolicyShadow(c *gin.Context) {
	report, err := billing_policy.BuildLegacyMigrationCandidate(billing_policy.StateShadow)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error(), "data": report})
		return
	}
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
	if stats.Observations == 0 || stats.Mismatches != 0 {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "shadow verification has no observations or still contains mismatches", "data": stats})
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

func persistBillingPolicyConfig(config billing_policy.Config) error {
	if err := billing_policy.ValidateConfig(&config); err != nil {
		return err
	}
	data, err := common.Marshal(config)
	if err != nil {
		return err
	}
	if err := model.UpdateOption("ModelBillingPolicy", string(data)); err != nil {
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
