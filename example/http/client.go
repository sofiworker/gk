package main

import (
	"gk/glog"
)

func main() {
	glog.GetLogger().Info("hello world", glog.Field{
		Key:   "name",
		Value: "gk",
	})

	logger := glog.GetLogger().Module("test")
	logger.Infof("11111111111")
}
