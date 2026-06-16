package controller

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type lotteryPeriodRequest struct {
	Issue          int    `json:"issue"`
	Title          string `json:"title"`
	StartTime      int64  `json:"start_time"`
	EndTime        int64  `json:"end_time"`
	DisplayEnabled bool   `json:"display_enabled"`
}

type lotteryPrizeRequest struct {
	LevelName   string `json:"level_name"`
	Quantity    int    `json:"quantity"`
	Description string `json:"description"`
	PaidOnly    bool   `json:"paid_only"`
	SortOrder   int    `json:"sort_order"`
}

type lotteryImportCodesRequest struct {
	Codes string `json:"codes"`
}

type lotteryAdminPeriodDetail struct {
	Period model.LotteryPeriod      `json:"period"`
	Prizes []model.LotteryPrize     `json:"prizes"`
	Codes  []model.LotteryPrizeCode `json:"codes"`
}

func ListLotteryPeriods(c *gin.Context) {
	var periods []model.LotteryPeriod
	if err := model.DB.Where("display_enabled = ?", true).Order("issue desc, id desc").Find(&periods).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":        model.FillLotteryPeriodStats(periods),
		"current_time": common.GetTimestamp(),
	})
}

func GetLotteryPeriod(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的期数")
		return
	}
	result, err := model.GetLotteryPublicPeriod(id, c.GetInt("id"))
	if err != nil {
		common.ApiErrorMsg(c, "抽奖活动不存在或未展示")
		return
	}
	common.ApiSuccess(c, result)
}

func JoinLotteryPeriod(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的期数")
		return
	}
	userId := c.GetInt("id")
	username := c.GetString("username")
	if userId <= 0 {
		common.ApiErrorMsg(c, "请先登录后再参与抽奖")
		return
	}

	var entry model.LotteryEntry
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var period model.LotteryPeriod
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND display_enabled = ?", id, true).First(&period).Error; err != nil {
			return errors.New("抽奖活动不存在或未展示")
		}
		now := common.GetTimestamp()
		if period.Status == model.LotteryPeriodStatusDrawn || period.Status == model.LotteryPeriodStatusDrawing {
			return errors.New("本期抽奖已截止")
		}
		if period.StartTime > now {
			return errors.New("抽奖尚未开始")
		}
		if period.EndTime <= now {
			return errors.New("本期抽奖已截止")
		}

		if err := tx.Where("period_id = ? AND user_id = ?", period.Id, userId).First(&entry).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		entry = model.LotteryEntry{
			PeriodId: period.Id,
			UserId:   userId,
			Username: username,
		}
		return tx.Create(&entry).Error
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, entry)
}

func ScratchLotteryWinnerCode(c *gin.Context) {
	periodId, _ := strconv.Atoi(c.Param("id"))
	if periodId <= 0 {
		common.ApiErrorMsg(c, "无效的期数")
		return
	}
	userId := c.GetInt("id")
	if userId <= 0 {
		common.ApiErrorMsg(c, "请先登录后再查看奖品")
		return
	}

	var winner model.LotteryWinner
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var period model.LotteryPeriod
		if err := tx.Where("id = ? AND display_enabled = ?", periodId, true).First(&period).Error; err != nil {
			return errors.New("抽奖活动不存在或未展示")
		}
		if period.Status != model.LotteryPeriodStatusDrawn {
			return errors.New("本期尚未开奖")
		}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").
			Where("period_id = ? AND user_id = ?", periodId, userId).
			First(&winner).Error; err != nil {
			return errors.New("未查询到中奖记录")
		}
		if winner.CodeScratched {
			return nil
		}
		now := common.GetTimestamp()
		if err := tx.Model(&model.LotteryWinner{}).
			Where("id = ?", winner.Id).
			Updates(map[string]interface{}{
				"code_scratched":    true,
				"code_scratched_at": now,
			}).Error; err != nil {
			return err
		}
		winner.CodeScratched = true
		winner.CodeScratchedAt = now
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, winner)
}

func AdminListLotteryPeriods(c *gin.Context) {
	var periods []model.LotteryPeriod
	if err := model.DB.Order("issue desc, id desc").Find(&periods).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, model.FillLotteryPeriodStats(periods))
}

func AdminGetLotteryPeriod(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的期数")
		return
	}
	var period model.LotteryPeriod
	if err := model.DB.Where("id = ?", id).First(&period).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.FillLotteryPeriodStat(&period)

	var prizes []model.LotteryPrize
	if err := model.DB.Where("period_id = ?", id).Order("sort_order asc, id asc").Find(&prizes).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	prizes = model.FillLotteryPrizeCodeCounts(prizes)

	var codes []model.LotteryPrizeCode
	if err := model.DB.Where("period_id = ?", id).Order("prize_id asc, id asc").Find(&codes).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, lotteryAdminPeriodDetail{Period: period, Prizes: prizes, Codes: codes})
}

func AdminCreateLotteryPeriod(c *gin.Context) {
	var req lotteryPeriodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := validateLotteryPeriodRequest(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Issue <= 0 {
		nextIssue, err := model.NextLotteryIssue()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		req.Issue = nextIssue
	}
	period := model.LotteryPeriod{
		Issue:          req.Issue,
		Title:          strings.TrimSpace(req.Title),
		StartTime:      req.StartTime,
		EndTime:        req.EndTime,
		DisplayEnabled: req.DisplayEnabled,
		Status:         model.LotteryPeriodStatusDraft,
	}
	if err := model.DB.Create(&period).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, period)
}

func AdminUpdateLotteryPeriod(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的期数")
		return
	}
	var req lotteryPeriodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := validateLotteryPeriodRequest(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var period model.LotteryPeriod
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", id).First(&period).Error; err != nil {
			return err
		}
		if period.Status == model.LotteryPeriodStatusDrawn {
			return errors.New("已开奖期数不允许修改")
		}
		updateMap := map[string]interface{}{
			"title":           strings.TrimSpace(req.Title),
			"start_time":      req.StartTime,
			"end_time":        req.EndTime,
			"display_enabled": req.DisplayEnabled,
			"updated_at":      common.GetTimestamp(),
		}
		if req.Issue > 0 {
			updateMap["issue"] = req.Issue
		}
		return tx.Model(&model.LotteryPeriod{}).Where("id = ?", id).Updates(updateMap).Error
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminDeleteLotteryPeriod(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的期数")
		return
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var period model.LotteryPeriod
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", id).First(&period).Error; err != nil {
			return err
		}
		if period.Status == model.LotteryPeriodStatusDrawn {
			return errors.New("已开奖期数不允许删除")
		}
		if err := tx.Where("period_id = ?", id).Delete(&model.LotteryPrizeCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("period_id = ?", id).Delete(&model.LotteryPrize{}).Error; err != nil {
			return err
		}
		if err := tx.Where("period_id = ?", id).Delete(&model.LotteryEntry{}).Error; err != nil {
			return err
		}
		return tx.Delete(&period).Error
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminCreateLotteryPrize(c *gin.Context) {
	periodId, _ := strconv.Atoi(c.Param("id"))
	if periodId <= 0 {
		common.ApiErrorMsg(c, "无效的期数")
		return
	}
	var req lotteryPrizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := validateLotteryPrizeRequest(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := ensureLotteryPeriodEditable(periodId); err != nil {
		common.ApiError(c, err)
		return
	}
	prize := model.LotteryPrize{
		PeriodId:    periodId,
		LevelName:   strings.TrimSpace(req.LevelName),
		Quantity:    req.Quantity,
		Description: strings.TrimSpace(req.Description),
		PaidOnly:    req.PaidOnly,
		SortOrder:   req.SortOrder,
	}
	if err := model.DB.Create(&prize).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, prize)
}

func AdminUpdateLotteryPrize(c *gin.Context) {
	periodId, _ := strconv.Atoi(c.Param("id"))
	prizeId, _ := strconv.Atoi(c.Param("prize_id"))
	if periodId <= 0 || prizeId <= 0 {
		common.ApiErrorMsg(c, "无效的奖品")
		return
	}
	var req lotteryPrizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := validateLotteryPrizeRequest(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := ensureLotteryPeriodEditable(periodId); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Model(&model.LotteryPrize{}).
		Where("id = ? AND period_id = ?", prizeId, periodId).
		Updates(map[string]interface{}{
			"level_name":  strings.TrimSpace(req.LevelName),
			"quantity":    req.Quantity,
			"description": strings.TrimSpace(req.Description),
			"paid_only":   req.PaidOnly,
			"sort_order":  req.SortOrder,
			"updated_at":  common.GetTimestamp(),
		}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminDeleteLotteryPrize(c *gin.Context) {
	periodId, _ := strconv.Atoi(c.Param("id"))
	prizeId, _ := strconv.Atoi(c.Param("prize_id"))
	if periodId <= 0 || prizeId <= 0 {
		common.ApiErrorMsg(c, "无效的奖品")
		return
	}
	if err := ensureLotteryPeriodEditable(periodId); err != nil {
		common.ApiError(c, err)
		return
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("period_id = ? AND prize_id = ?", periodId, prizeId).Delete(&model.LotteryPrizeCode{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND period_id = ?", prizeId, periodId).Delete(&model.LotteryPrize{}).Error
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminImportLotteryPrizeCodes(c *gin.Context) {
	periodId, _ := strconv.Atoi(c.Param("id"))
	prizeId, _ := strconv.Atoi(c.Param("prize_id"))
	if periodId <= 0 || prizeId <= 0 {
		common.ApiErrorMsg(c, "无效的奖品")
		return
	}
	var req lotteryImportCodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	codes := model.NormalizeLotteryCodes(req.Codes)
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var period model.LotteryPeriod
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", periodId).First(&period).Error; err != nil {
			return err
		}
		if period.Status == model.LotteryPeriodStatusDrawn {
			return errors.New("已开奖期数不允许导入兑换码")
		}
		var prize model.LotteryPrize
		if err := tx.Where("id = ? AND period_id = ?", prizeId, periodId).First(&prize).Error; err != nil {
			return err
		}
		if len(codes) != prize.Quantity {
			return fmtCodeCountError(prize.LevelName)
		}
		if err := tx.Where("period_id = ? AND prize_id = ?", periodId, prizeId).Delete(&model.LotteryPrizeCode{}).Error; err != nil {
			return err
		}
		for _, code := range codes {
			if err := tx.Create(&model.LotteryPrizeCode{
				PeriodId: periodId,
				PrizeId:  prizeId,
				Code:     code,
				Status:   model.LotteryCodeStatusUnused,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"count": len(codes)})
}

func AdminDrawLotteryPeriod(c *gin.Context) {
	periodId, _ := strconv.Atoi(c.Param("id"))
	winners, err := service.DrawLotteryPeriod(periodId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"winners": winners})
}

func validateLotteryPeriodRequest(req *lotteryPeriodRequest) error {
	if req.StartTime <= 0 || req.EndTime <= 0 {
		return errors.New("请设置启动时间和截止时间")
	}
	if req.EndTime <= req.StartTime {
		return errors.New("截止时间必须晚于启动时间")
	}
	if req.Issue < 0 {
		return errors.New("期数不能为负数")
	}
	return nil
}

func validateLotteryPrizeRequest(req *lotteryPrizeRequest) error {
	if strings.TrimSpace(req.LevelName) == "" {
		return errors.New("奖项名称不能为空")
	}
	if req.Quantity <= 0 {
		return errors.New("奖品数量必须大于 0")
	}
	return nil
}

func ensureLotteryPeriodEditable(periodId int) error {
	var period model.LotteryPeriod
	if err := model.DB.Where("id = ?", periodId).First(&period).Error; err != nil {
		return err
	}
	if period.Status == model.LotteryPeriodStatusDrawn {
		return errors.New("已开奖期数不允许修改")
	}
	return nil
}

func fmtCodeCountError(levelName string) error {
	return errors.New(levelName + " 的兑换码数量必须等于奖品数量")
}
