package zipkin_graphql

import (
	"github.com/Laisky/go-utils"
	"github.com/Laisky/zap"
	"gopkg.in/olivere/elastic.v5"
)

var esclient *elastic.Client

func SetupESClient() {
	var err error
	if esclient, err = elastic.NewClient(
		elastic.SetURL(utils.Settings.GetStringSlice("esapis")...),
	); err != nil {
		utils.Logger.Panic("try to create es client got error", zap.Error(err))
	}
}
