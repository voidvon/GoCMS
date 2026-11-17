package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gocms/internal/nginxguard"
	"gocms/internal/security"
)

func runNginx(args []string) error {
	if len(args) == 0 || args[0] != "setup" {
		return errors.New("用法：./gocms nginx setup [-db /绝对路径/site.db]")
	}
	return setupNginx(args[1:])
}

func setupNginx(args []string) error {
	flags := flag.NewFlagSet("nginx setup", flag.ContinueOnError)
	databasePath := flags.String("db", runtimeDefaults().database, "正在运行的 GoCMS 使用的 SQLite 数据库路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("用法：./gocms nginx setup [-db /绝对路径/site.db]")
	}
	if os.Geteuid() != 0 {
		return errors.New("请以 root 执行 ./gocms nginx setup；日常 GoCMS 继续以 www 等普通用户运行")
	}
	path, err := filepath.Abs(*databasePath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("无法读取已有 GoCMS 数据库，请先在后台保存 Nginx 接入设置，或用 -db 指定正在使用的数据库：%w", err)
	}
	// Read-only avoids creating a second database or root-owned SQLite journals.
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	database, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	config, snapshot, err := security.NginxInitialization(ctx, database)
	if err != nil {
		return fmt.Errorf("读取后台已保存的 Nginx 接入设置失败：%w", err)
	}
	fmt.Printf("配置 GoCMS 的 Nginx 接入：\n运行用户：%s\n可执行文件：%s\n主配置：%s\n安全规则文件（不存在时自动创建）：%s\n", config.RunUser, config.NginxBinary, config.NginxConfig, config.ServerConfig)
	if err := nginxguard.Initialize(ctx, config, snapshot); err != nil {
		return err
	}
	fmt.Println("Nginx 接入配置完成。返回后台点击“立即同步”即可；GoCMS 继续以普通系统用户运行，无需其他常驻服务。")
	return nil
}
