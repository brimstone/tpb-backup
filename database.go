package main

import (
	beegoorm "github.com/astaxie/beego/orm"
	_ "github.com/mattn/go-sqlite3"
)

var orm beegoorm.Ormer

var debug bool

// InitDatabase sets up the database and beegoorm
func InitDatabase() error {
	var err error
	beegoorm.Debug = debug

	// Migrate our database if needed
	beegoorm.RegisterModel(new(Category))
	beegoorm.RegisterModel(new(Tag))
	beegoorm.RegisterModel(new(Uploader))
	beegoorm.RegisterModel(new(Torrent))

	err = beegoorm.RegisterDataBase("default", "sqlite3", "tpb.db", 1, 1)

	err = beegoorm.RunSyncdb("default", false, debug)

	if err != nil {
		return err
	}

	orm = beegoorm.NewOrm()

	return nil
}
