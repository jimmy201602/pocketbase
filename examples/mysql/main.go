package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"time"

	pocketbase "github.com/AlperRehaYAZGAN/postgresbase"
	"github.com/AlperRehaYAZGAN/postgresbase/core"
	"github.com/AlperRehaYAZGAN/postgresbase/plugins/migratecmd"
	"github.com/spf13/viper"
	"github.com/toolkits/pkg/file"
)

type ConfigT struct {
	JwtPrivateKey string `yaml:"jwtPrivateKey"`
	JwtPublicKey  string `yaml:"jwtPublicKey"`
	BcryptCost      string `yaml:"bcryptCost"`
	LogsDatabase string `yaml:"logsDatabase"`
	DATABASE     string `yaml:"database"`
}

var (
	Config ConfigT
)

func parse(conf string) error {
	bs, err := file.ReadBytes(conf)
	if err != nil {
		return fmt.Errorf("cannot read yml[%s]: %v", conf, err)
	}

	viper.SetConfigType("yaml")
	err = viper.ReadConfig(bytes.NewBuffer(bs))
	if err != nil {
		return fmt.Errorf("cannot read yml[%s]: %v", conf, err)
	}

	var c ConfigT
	err = viper.Unmarshal(&c)
	if err != nil {
		return fmt.Errorf("unmarshal config error:%v", err)
	}
	pkb, err := file.ReadBytes(c.JwtPrivateKey)
	if err != nil {
		return fmt.Errorf("cannot read private key: %s",c.JwtPrivateKey)
	}
	c.JwtPrivateKey = string(pkb)
	pub, err := file.ReadBytes(c.JwtPublicKey)
	if err != nil {
		return fmt.Errorf("cannot read public key: %s",c.JwtPublicKey)
	}
	c.JwtPublicKey = string(pub)
	Config = c
	return nil
}

func parseConf() {
	if err := parse("etc/pocketbase.yml"); err != nil {
		fmt.Println("cannot parse configuration file:", err)
		os.Exit(1)
	}
}

func main() {
	parseConf()
	os.Setenv("JWT_PRIVATE_KEY", Config.JwtPrivateKey)
	os.Setenv("JWT_PUBLIC_KEY", Config.JwtPublicKey)
	os.Setenv("BCRYPT_COST", Config.BcryptCost)
	os.Setenv("LOGS_DATABASE", Config.LogsDatabase)
	os.Setenv("DATABASE", Config.DATABASE)

	app := pocketbase.New()

	// ---------------------------------------------------------------
	// Optional plugin flags:
	// ---------------------------------------------------------------
	var automigrate bool
	app.RootCmd.PersistentFlags().BoolVar(
		&automigrate,
		"automigrate",
		false,
		"enable/disable auto migrations",
	)

	var queryTimeout int
	app.RootCmd.PersistentFlags().IntVar(
		&queryTimeout,
		"queryTimeout",
		30,
		"the default SELECT queries timeout in seconds",
	)

	app.RootCmd.ParseFlags(os.Args[1:])

	// ---------------------------------------------------------------
	// Plugins and hooks:
	// ---------------------------------------------------------------

	// migrate command (with js templates)
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		TemplateLang: migratecmd.TemplateLangJS,
		Automigrate:  automigrate,
		// Dir:          migrationsDir,
	})

	// // GitHub selfupdate
	// ghupdate.MustRegister(app, app.RootCmd, ghupdate.Config{})

	app.OnAfterBootstrap().PreAdd(func(e *core.BootstrapEvent) error {
		app.Dao().ModelQueryTimeout = time.Duration(queryTimeout) * time.Second
		return nil
	})

	// app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
	// 	// serves static files from the provided public dir (if exists)
	// 	e.Router.GET("/*", apis.StaticDirectoryHandler(os.DirFS(publicDir), indexFallback))
	// 	return nil
	// })

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
