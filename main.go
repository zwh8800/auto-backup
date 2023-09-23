package main

import (
	_ "embed"
	"encoding/json"
	"os"
	"path"

	"github.com/getlantern/systray"
	"github.com/sqweek/dialog"
)

const configFileName = "AutoBackup.json"

var (
	src  string
	dest string
)

//go:embed icon.ico
var icon []byte

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	loadConfig()

	systray.SetIcon(icon)
	systray.SetTitle("自动备份文件")
	systray.SetTooltip("自动备份文件")
	mGetPath := systray.AddMenuItem("查看备份目录", "查看备份目录")
	mSetPath := systray.AddMenuItem("设置备份目录", "设置备份目录")
	mQuit := systray.AddMenuItem("退出", "退出程序")

	go onGetPathBtnClick(mGetPath)
	go onSetPathBtnClick(mSetPath)
	go onQuitBtnClick(mQuit)

	doAutoBackup()
}

func onGetPathBtnClick(mGetPath *systray.MenuItem) {
	for {
		<-mGetPath.ClickedCh

		dialog.Message("源目录：%s", src).Title("源目录").Info()
		dialog.Message("备份目录：%s", dest).Title("备份目录").Info()
	}
}

func onSetPathBtnClick(mSetPath *systray.MenuItem) {
	for {
		<-mSetPath.ClickedCh

		directory, err := dialog.Directory().SetStartDir(src).Title("配置源目录").Browse()
		if err != nil {
			continue
		}
		src = directory

		directory, err = dialog.Directory().SetStartDir(dest).Title("配置备份目录").Browse()
		if err != nil {
			continue
		}
		dest = directory

		saveConfig()
	}
}

func onQuitBtnClick(mQuit *systray.MenuItem) {
	for {
		<-mQuit.ClickedCh
		os.Exit(0)
	}
}

type configFile struct {
	Src  string
	Dest string
}

func loadConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path.Join(home, ".config", configFileName))
	if err != nil {
		return
	}
	var conf configFile
	err = json.Unmarshal(data, &conf)
	if err != nil {
		return
	}

	src = conf.Src
	dest = conf.Dest
}

func saveConfig() {
	var conf configFile
	conf.Src = src
	conf.Dest = dest

	data, err := json.Marshal(&conf)
	if err != nil {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	configDir := path.Join(home, ".config")

	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		os.MkdirAll(configDir, 0755)
	}

	err = os.WriteFile(path.Join(configDir, configFileName), data, 0644)
	if err != nil {
		return
	}
}

func doAutoBackup() {

}

func onExit() {

}
