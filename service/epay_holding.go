package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

func StartEpayOrderExpiryTask() {
	if !common.IsMasterNode {
		return
	}
	gopool.Go(func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if err := model.ExpireDueEpayOrders(time.Now().Unix(), 200); err != nil {
				common.SysError("expire epay orders: " + err.Error())
			}
			<-ticker.C
		}
	})
}
