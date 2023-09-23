package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"os"
	"path"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/getlantern/systray"
	cp "github.com/otiai10/copy"
	"github.com/sqweek/dialog"
	"github.com/yudppp/throttle"
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
	backup()
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
		doAutoBackup()
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

var watcher *fsnotify.Watcher

func init() {
	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		dialog.Message("NewWatcher err: %s", err.Error()).Title("NewWatcher err").Error()
		log.Fatal(err)
	}

	// Start listening for events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Println("event:", event)

				backup()
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
				dialog.Message("watcher err: %s", err.Error()).Title("watcher err").Error()
			}
		}
	}()

}

var mu sync.Mutex

func doAutoBackup() {
	mu.Lock()
	defer mu.Unlock()

	for _, p := range watcher.WatchList() {
		watcher.Remove(p)
	}

	err := watcher.Add(src)
	if err != nil {
		log.Fatal(err)
		dialog.Message("watcher Add err: %s", err.Error()).Title("watcher Add err").Error()
	}
}

var backupThrottler = throttle.New(time.Second)

func backup() {
	backupThrottler.Do(func() {
		log.Println("start copy")
		defer log.Println("end copy")
		err := cp.Copy(src, dest)
		if err != nil {
			log.Println("copy err:", err)
		}
	})
}

func onExit() {
	watcher.Close()
}
