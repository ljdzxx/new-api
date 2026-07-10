package billing_policy

import (
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

type RequestContext struct {
	Headers map[string]string
	Body    []byte
	Now     time.Time
}

type AppliedAdjustment struct {
	ID         string `json:"id"`
	Multiplier string `json:"multiplier"`
}

func EvaluateAdjustments(policy Policy, ctx RequestContext) (decimal.Decimal, []AppliedAdjustment) {
	multiplier := decimal.NewFromInt(1)
	applied := make([]AppliedAdjustment, 0)
	for _, adjustment := range policy.Adjustments {
		matched := true
		for _, condition := range adjustment.Conditions {
			if !matchAdjustmentCondition(condition, ctx) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		value, err := decimal.NewFromString(adjustment.Multiplier)
		if err != nil || !value.IsPositive() {
			continue
		}
		multiplier = multiplier.Mul(value)
		applied = append(applied, AppliedAdjustment{ID: adjustment.ID, Multiplier: value.String()})
	}
	return multiplier, applied
}

func matchAdjustmentCondition(condition AdjustmentCondition, ctx RequestContext) bool {
	var actual string
	exists := false
	switch condition.Source {
	case "header":
		actual, exists = ctx.Headers[strings.ToLower(condition.Path)]
	case "param":
		result := gjson.GetBytes(ctx.Body, condition.Path)
		exists = result.Exists()
		actual = result.String()
	case "hour", "weekday":
		location, err := time.LoadLocation(condition.Timezone)
		if err != nil {
			return false
		}
		now := ctx.Now
		if now.IsZero() {
			now = time.Now()
		}
		now = now.In(location)
		exists = true
		if condition.Source == "hour" {
			actual = strconv.Itoa(now.Hour())
		} else {
			actual = strconv.Itoa(int(now.Weekday()))
		}
	}
	if condition.Operator == "exists" {
		return exists
	}
	if !exists {
		return false
	}
	switch condition.Operator {
	case "eq":
		return actual == condition.Value
	case "contains":
		return strings.Contains(actual, condition.Value)
	}
	left, leftErr := strconv.ParseFloat(actual, 64)
	right, rightErr := strconv.ParseFloat(condition.Value, 64)
	if leftErr != nil || rightErr != nil {
		return false
	}
	switch condition.Operator {
	case "lt":
		return left < right
	case "lte":
		return left <= right
	case "gt":
		return left > right
	case "gte":
		return left >= right
	default:
		return false
	}
}
