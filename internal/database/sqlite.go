package database

import (
	"fmt"
	"log"

	"deploy-manager/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Init(dbPath string) error {
	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}

	if err := DB.AutoMigrate(
		&model.User{},
		&model.Server{},
		&model.Deployment{},
		&model.CronJob{},
		&model.CronHistory{},
		&model.FileOperation{},
		&model.InfrastructureScenario{},
		&model.InfrastructureExecution{},
		&model.Database{},
		&model.DatabaseQuery{},
		&model.Project{},
		&model.ProjectComponent{},
		&model.ComposeTemplate{},
		&model.Note{},
		&model.Command{},
		&model.TerminalSessionLog{},
	); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("Database initialized successfully")
	return nil
}

func Close() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
