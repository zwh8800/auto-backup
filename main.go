package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gen2brain/dlgs"
	"github.com/getlantern/systray"
	cp "github.com/otiai10/copy"
	"github.com/sqweek/dialog"
	"github.com/studio-b12/gowebdav"
	"github.com/yudppp/throttle"
)

const configFileName = "AutoBackup.json"

const (
	DestTypeFS     = "文件系统"
	DestTypeWebDAV = "WebDAV"
)

var (
	src      string
	destType string
	dest     string
)

//go:embed icon.ico
var icon []byte

func main() {
	systray.Run(onReady, onExit)
}

var mLastBackup *systray.MenuItem

func setLastBackup(t time.Time) {
	if mLastBackup != nil {
		mLastBackup.SetTitle(fmt.Sprintf("上次备份：%s", t.Format("2006-01-02 15:04")))
		mLastBackup.SetTooltip(fmt.Sprintf("上次备份：%s", t.Format("2006-01-02 15:04")))
	}
}

func onReady() {
	loadConfig()

	systray.SetIcon(icon)
	systray.SetTitle("自动备份文件")
	systray.SetTooltip("自动备份文件")
	mLastBackup = systray.AddMenuItem("上次备份：未备份", "上次备份：未备份")
	mGetPath := systray.AddMenuItem("查看备份目录", "查看备份目录")
	mSetPath := systray.AddMenuItem("设置备份目录", "设置备份目录")
	mPullBackup := systray.AddMenuItem("拉取备份", "拉取备份")
	mPushBackup := systray.AddMenuItem("推送备份", "推送备份")
	mQuit := systray.AddMenuItem("退出", "退出程序")

	go onGetPathBtnClick(mGetPath)
	go onSetPathBtnClick(mSetPath)
	go onPullBackupBtnClick(mPullBackup)
	go onPushBackupBtnClick(mPushBackup)
	go onQuitBtnClick(mQuit)

	doAutoBackup()
}

func onGetPathBtnClick(btn *systray.MenuItem) {
	for {
		<-btn.ClickedCh

		dialog.Message("源目录：%s", src).Title("源目录").Info()
		dialog.Message("备份类型：%s，备份目录：%s", destType, dest).Title("备份目录").Info()
	}
}

func onSetPathBtnClick(btn *systray.MenuItem) {
	for {
		<-btn.ClickedCh

		directory, err := dialog.Directory().SetStartDir(src).Title("配置源目录").Browse()
		if err != nil {
			log.Println("dialog err:", err)
			continue
		}
		src = directory

		item, ok, err := dlgs.List("备份类型", "请选择备份类型:", []string{"文件系统", "WebDAV"})
		if err != nil {
			log.Println("dialog err:", err)
			continue
		}
		if !ok {
			continue
		}

		destType = item

		switch destType {
		case DestTypeFS:
			directory, err = dialog.Directory().SetStartDir(dest).Title("配置备份目录").Browse()
			if err != nil {
				log.Println("dialog err:", err)
				continue
			}
			dest = directory
		case DestTypeWebDAV:
			item, ok, err := dlgs.Entry("配置地址", "请输入WebDAV地址", dest)
			if err != nil {
				log.Println("dialog err:", err)
				continue
			}
			if !ok {
				continue
			}
			dest = item
		}

		saveConfig()
		doAutoBackup()
	}
}

func onPullBackupBtnClick(btn *systray.MenuItem) {
	for {
		<-btn.ClickedCh
		ok, _ := dlgs.Question("确定要拉取备份？", "确定要拉取备份？", true)
		if !ok {
			continue
		}

		err := cp.Copy(src, src+"-backup", cp.Options{PreserveTimes: true})
		if err != nil {
			dialog.Message("backup src err: %s", err.Error()).Title("backup src err").Error()
			continue
		}

		switch destType {
		case DestTypeFS:
			err := cp.Copy(dest, src, cp.Options{PreserveTimes: true})
			if err != nil {
				log.Println("copy err:", err)
				dialog.Message("pull Copy err: %s", err.Error()).Title("pull Copy err").Error()
			}
		case DestTypeWebDAV:
			err := pullWebdav(src, dest)
			if err != nil {
				log.Println("pullWebdav err:", err)
				dialog.Message("pull pullWebdav err: %s", err.Error()).Title("pull pullWebdav err").Error()
			}
		}

	}
}

func onPushBackupBtnClick(btn *systray.MenuItem) {
	for {
		<-btn.ClickedCh
		backup()
	}
}

func onQuitBtnClick(btn *systray.MenuItem) {
	for {
		<-btn.ClickedCh
		os.Exit(0)
	}
}

type configFile struct {
	Src      string
	DestType string
	Dest     string
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
	destType = conf.DestType
	dest = conf.Dest
}

func saveConfig() {
	var conf configFile
	conf.Src = src
	conf.DestType = destType
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

var throttleDur = 1 * time.Second

var backupThrottler = throttle.New(throttleDur)

func backup() {
	backupThrottler.Do(func() {
		time.Sleep(throttleDur)
		log.Println("start copy")
		defer log.Println("end copy")
		defer setLastBackup(time.Now())

		switch destType {
		case DestTypeFS:
			err := cp.Copy(src, dest, cp.Options{PreserveTimes: true})
			if err != nil {
				log.Println("copy err:", err)
				dialog.Message("backup Copy err: %s", err.Error()).Title("backup Copy err").Error()
			}
		case DestTypeWebDAV:
			err := copyWebdav(src, dest)
			if err != nil {
				log.Println("copy err:", err)
				dialog.Message("backup Copy err: %s", err.Error()).Title("backup Copy err").Error()
			}
		}

	})
}

func copyWebdav(src, dest string) error {
	u, err := url.Parse(dest)
	if err != nil {
		return err
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	u.User = nil
	basePath := u.Path
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""

	c := gowebdav.NewClient(u.String(), user, pass)
	err = c.Connect()
	if err != nil {
		return err
	}

	err = c.MkdirAll(basePath, 0755)
	if err != nil {
		return err
	}

	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		log.Println("copy", path)

		p, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		webdavPath := filepath.ToSlash(filepath.Join(basePath, p))

		if d.IsDir() {
			err = c.MkdirAll(webdavPath, 0755)
			if err != nil {
				return err
			}
		} else {
			f, err := os.Open(path)
			if err != nil {
				return err
			}

			err = c.WriteStream(webdavPath, f, 0644)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func pullWebdav(src, dest string) error {
	// pull is from dest to src
	u, err := url.Parse(dest)
	if err != nil {
		return err
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	u.User = nil
	basePath := u.Path
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""

	c := gowebdav.NewClient(u.String(), user, pass)
	err = c.Connect()
	if err != nil {
		return err
	}

	err = os.MkdirAll(src, 0755)
	if err != nil {
		return err
	}

	files, err := c.ReadDir(basePath)
	if err != nil {
		return err
	}
	for _, file := range files {
		log.Println("pull", file.Name())

		err := pullSingleWebdav(basePath, file, src, c)
		if err != nil {
			return err
		}
	}

	return nil
}

func pullSingleWebdav(basePath string, file fs.FileInfo, src string, c *gowebdav.Client) error {
	p := file.Name()
	localPath := filepath.Join(src, p)
	remotePath := filepath.ToSlash(filepath.Join(basePath, file.Name()))

	if file.IsDir() {
		u, err := url.Parse(dest)
		if err != nil {
			return err
		}
		u.Path = path.Join(basePath, file.Name())

		err = pullWebdav(localPath, u.String())
		if err != nil {
			return err
		}
	} else {
		reader, err := c.ReadStream(remotePath)
		if err != nil {
			return err
		}

		file, err := os.Create(localPath)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(file, reader)
		if err != nil {
			return err
		}
	}
	return nil
}

func onExit() {
	watcher.Close()
}
