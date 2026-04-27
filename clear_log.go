package main

import (
	"log"
	"deploy-manager/internal/config"
	"deploy-manager/internal/database"
)

func main() {
	_, err := config.Load("artifacts/config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	if err := database.Init(config.GetDBPath()); err != nil {
		log.Fatal(err)
	}
	database.DB.Exec("UPDATE project_components SET deploy_log = ''")
	log.Println("已清空部署日志")
}
