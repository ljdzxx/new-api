package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	InviteRewardAuditStatusGranted          = "granted"
	InviteRewardAuditStatusDeniedRisk       = "denied_risk"
	InviteRewardAuditStatusDeniedDailyLimit = "denied_daily_limit"
	InviteRewardAuditStatusDeniedPolicy     = "denied_policy"
)

type RegistrationFingerprint struct {
	FingerprintHash string `json:"fingerprint_hash"`
	CanvasHash      string `json:"canvas_hash"`
	WebGLHash       string `json:"webgl_hash"`
	AudioHash       string `json:"audio_hash"`
	FontsHash       string `json:"fonts_hash"`
	UAHash          string `json:"ua_hash"`
	LocaleHash      string `json:"locale_hash"`
	ScreenHash      string `json:"screen_hash"`
	HardwareHash    string `json:"hardware_hash"`
	Missing         bool   `json:"missing"`
}

type UserRegistrationProfile struct {
	Id                 int    `json:"id"`
	UserId             int    `json:"user_id" gorm:"uniqueIndex"`
	IPHash             string `json:"-" gorm:"type:varchar(128);index"`
	FingerprintHash    string `json:"-" gorm:"type:varchar(128);index"`
	CanvasHash         string `json:"-" gorm:"type:varchar(128);index"`
	WebGLHash          string `json:"-" gorm:"type:varchar(128);index"`
	AudioHash          string `json:"-" gorm:"type:varchar(128);index"`
	FontsHash          string `json:"-" gorm:"type:varchar(128);index"`
	UAHash             string `json:"-" gorm:"type:varchar(128);index"`
	LocaleHash         string `json:"-" gorm:"type:varchar(128);index"`
	ScreenHash         string `json:"-" gorm:"type:varchar(128);index"`
	HardwareHash       string `json:"-" gorm:"type:varchar(128);index"`
	FingerprintMissing bool   `json:"fingerprint_missing" gorm:"default:false"`
	CreatedAt          int64  `json:"created_at" gorm:"type:bigint;index"`
}

type InviteRewardAudit struct {
	Id               int    `json:"id"`
	InviterId        int    `json:"inviter_id" gorm:"index"`
	InviteeId        int    `json:"invitee_id" gorm:"index"`
	RiskScore        int    `json:"risk_score" gorm:"index"`
	RiskReasons      string `json:"risk_reasons" gorm:"type:text"`
	RewardStatus     string `json:"reward_status" gorm:"type:varchar(32);index"`
	DailyCountBefore int    `json:"daily_count_before"`
	CreatedAt        int64  `json:"created_at" gorm:"type:bigint;index"`
}

type InviteRewardAuditFilter struct {
	InviterId    int
	InviteeId    int
	RewardStatus string
	MinRiskScore int
	MaxRiskScore int
	StartTime    int64
	EndTime      int64
}

func hashInviteRiskValue(kind string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return common.GenerateHMAC("invite_risk:" + kind + ":" + value)
}

func SaveUserRegistrationProfile(userId int, ip string, fp RegistrationFingerprint) error {
	if userId <= 0 {
		return nil
	}
	profile := UserRegistrationProfile{
		UserId:             userId,
		IPHash:             hashInviteRiskValue("ip", ip),
		FingerprintHash:    hashInviteRiskValue("fingerprint", fp.FingerprintHash),
		CanvasHash:         hashInviteRiskValue("canvas", fp.CanvasHash),
		WebGLHash:          hashInviteRiskValue("webgl", fp.WebGLHash),
		AudioHash:          hashInviteRiskValue("audio", fp.AudioHash),
		FontsHash:          hashInviteRiskValue("fonts", fp.FontsHash),
		UAHash:             hashInviteRiskValue("ua", fp.UAHash),
		LocaleHash:         hashInviteRiskValue("locale", fp.LocaleHash),
		ScreenHash:         hashInviteRiskValue("screen", fp.ScreenHash),
		HardwareHash:       hashInviteRiskValue("hardware", fp.HardwareHash),
		FingerprintMissing: fp.Missing || strings.TrimSpace(fp.FingerprintHash) == "",
		CreatedAt:          common.GetTimestamp(),
	}
	result := DB.Model(&UserRegistrationProfile{}).Where("user_id = ?", userId).Updates(map[string]interface{}{
		"ip_hash":             profile.IPHash,
		"fingerprint_hash":    profile.FingerprintHash,
		"canvas_hash":         profile.CanvasHash,
		"web_gl_hash":         profile.WebGLHash,
		"audio_hash":          profile.AudioHash,
		"fonts_hash":          profile.FontsHash,
		"ua_hash":             profile.UAHash,
		"locale_hash":         profile.LocaleHash,
		"screen_hash":         profile.ScreenHash,
		"hardware_hash":       profile.HardwareHash,
		"fingerprint_missing": profile.FingerprintMissing,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return DB.Create(&profile).Error
}

func getUserRegistrationProfile(userId int) (*UserRegistrationProfile, error) {
	if userId <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var profile UserRegistrationProfile
	err := DB.Where("user_id = ?", userId).First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func addRiskScore(score *int, reasons *[]string, weight int, label string, left string, right string) {
	if weight <= 0 || left == "" || right == "" || left != right {
		return
	}
	*score += weight
	*reasons = append(*reasons, fmt.Sprintf("%s:%d", label, weight))
}

func calculateInviteRiskScore(inviterProfile, inviteeProfile *UserRegistrationProfile) (int, string) {
	if inviterProfile == nil || inviteeProfile == nil {
		return 0, "profile_missing"
	}
	weights := common.InviteRiskWeights
	score := 0
	reasons := make([]string, 0, 10)
	addRiskScore(&score, &reasons, weights.IP, "ip", inviterProfile.IPHash, inviteeProfile.IPHash)
	addRiskScore(&score, &reasons, weights.Fingerprint, "fingerprint", inviterProfile.FingerprintHash, inviteeProfile.FingerprintHash)
	addRiskScore(&score, &reasons, weights.Canvas, "canvas", inviterProfile.CanvasHash, inviteeProfile.CanvasHash)
	addRiskScore(&score, &reasons, weights.WebGL, "webgl", inviterProfile.WebGLHash, inviteeProfile.WebGLHash)
	addRiskScore(&score, &reasons, weights.Audio, "audio", inviterProfile.AudioHash, inviteeProfile.AudioHash)
	addRiskScore(&score, &reasons, weights.Fonts, "fonts", inviterProfile.FontsHash, inviteeProfile.FontsHash)
	addRiskScore(&score, &reasons, weights.UA, "ua", inviterProfile.UAHash, inviteeProfile.UAHash)
	addRiskScore(&score, &reasons, weights.Locale, "locale", inviterProfile.LocaleHash, inviteeProfile.LocaleHash)
	addRiskScore(&score, &reasons, weights.Screen, "screen", inviterProfile.ScreenHash, inviteeProfile.ScreenHash)
	addRiskScore(&score, &reasons, weights.Hardware, "hardware", inviterProfile.HardwareHash, inviteeProfile.HardwareHash)
	if score > 100 {
		score = 100
	}
	if len(reasons) == 0 {
		return score, "no_match"
	}
	return score, strings.Join(reasons, ",")
}

func todayTimestampRange() (int64, int64) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	return start, start + 86400
}

func countTodayGrantedInviteRewards(inviterId int) (int, error) {
	start, end := todayTimestampRange()
	var count int64
	err := DB.Model(&InviteRewardAudit{}).
		Where("inviter_id = ? AND reward_status = ? AND created_at >= ? AND created_at < ?", inviterId, InviteRewardAuditStatusGranted, start, end).
		Count(&count).Error
	return int(count), err
}

func evaluateInviteReward(user *User, inviterId int) (string, int, string, int) {
	status := InviteRewardAuditStatusGranted
	score := 0
	reasons := "passed"
	if user == nil || inviterId <= 0 {
		return status, score, reasons, 0
	}

	inviterProfile, inviterProfileErr := getUserRegistrationProfile(inviterId)
	inviteeProfile, inviteeProfileErr := getUserRegistrationProfile(user.Id)
	if user.RegistrationRiskScore != nil {
		score = *user.RegistrationRiskScore
		reasons = user.RegistrationRiskReason
		if strings.TrimSpace(reasons) == "" {
			reasons = "risk_token_abnormal"
		}
	} else if common.InviteRiskControlEnabled {
		if inviterProfileErr == nil && inviteeProfileErr == nil {
			score, reasons = calculateInviteRiskScore(inviterProfile, inviteeProfile)
		} else {
			reasons = "profile_missing"
		}
	} else if inviterProfileErr == nil && inviteeProfileErr == nil {
		score, reasons = calculateInviteRiskScore(inviterProfile, inviteeProfile)
	}
	if common.InviteRiskControlEnabled && score >= common.InviteRiskThreshold {
		status = InviteRewardAuditStatusDeniedRisk
	}

	dailyCountBefore, err := countTodayGrantedInviteRewards(inviterId)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to count today's invite rewards for user #%d: %s", inviterId, err.Error()))
	}
	if status == InviteRewardAuditStatusGranted && common.InviteRiskDailyLimit > 0 && dailyCountBefore >= common.InviteRiskDailyLimit {
		status = InviteRewardAuditStatusDeniedDailyLimit
		if reasons == "passed" || reasons == "no_match" {
			reasons = "daily_limit"
		} else {
			reasons += ",daily_limit"
		}
	}

	if status == InviteRewardAuditStatusGranted && !eligibleForInviteReward(user) {
		status = InviteRewardAuditStatusDeniedPolicy
		if reasons == "passed" || reasons == "no_match" {
			reasons = "policy_not_eligible"
		} else {
			reasons += ",policy_not_eligible"
		}
	}
	return status, score, reasons, dailyCountBefore
}

func createInviteRewardAudit(inviterId, inviteeId, score int, reasons, status string, dailyCountBefore int) {
	audit := InviteRewardAudit{
		InviterId:        inviterId,
		InviteeId:        inviteeId,
		RiskScore:        score,
		RiskReasons:      reasons,
		RewardStatus:     status,
		DailyCountBefore: dailyCountBefore,
		CreatedAt:        common.GetTimestamp(),
	}
	if err := DB.Create(&audit).Error; err != nil {
		common.SysLog(fmt.Sprintf("failed to create invite reward audit: %s", err.Error()))
	}
}

func GetInviteRewardAudits(pageInfo *common.PageInfo, filter InviteRewardAuditFilter) ([]*InviteRewardAudit, int64, error) {
	query := DB.Model(&InviteRewardAudit{})
	if filter.InviterId > 0 {
		query = query.Where("inviter_id = ?", filter.InviterId)
	}
	if filter.InviteeId > 0 {
		query = query.Where("invitee_id = ?", filter.InviteeId)
	}
	if filter.RewardStatus != "" {
		query = query.Where("reward_status = ?", filter.RewardStatus)
	}
	if filter.MinRiskScore > 0 {
		query = query.Where("risk_score >= ?", filter.MinRiskScore)
	}
	if filter.MaxRiskScore > 0 {
		query = query.Where("risk_score <= ?", filter.MaxRiskScore)
	}
	if filter.StartTime > 0 {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("created_at <= ?", filter.EndTime)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var audits []*InviteRewardAudit
	err := query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&audits).Error
	return audits, total, err
}

func FillUsersInviteRiskScore(users []*User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]int, 0, len(users))
	for _, user := range users {
		if user != nil && user.InviterId > 0 {
			ids = append(ids, user.Id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var audits []InviteRewardAudit
	if err := DB.Where("invitee_id IN ?", ids).Order("id desc").Find(&audits).Error; err != nil {
		return err
	}
	scoreByInvitee := make(map[int]int, len(audits))
	for _, audit := range audits {
		if _, ok := scoreByInvitee[audit.InviteeId]; ok {
			continue
		}
		scoreByInvitee[audit.InviteeId] = audit.RiskScore
	}
	for _, user := range users {
		if user == nil {
			continue
		}
		if score, ok := scoreByInvitee[user.Id]; ok {
			user.InviteRiskScore = &score
		}
	}
	return nil
}
