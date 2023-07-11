package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/lulugyf/sshserv/hh"
	"io"
	"net/http"

	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type App struct {
	Name        string            `json:"name"`
	Cwd         string            `json:"cwd"`
	Cmd         []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Cron        string            `json:"crontab"`
	CheckExists string            `json:"check_exists"`
}

func startApp(app *App, logdir string, home string) {
	var cmd *exec.Cmd = nil

	cmd = exec.Command(app.Cmd[0], app.Cmd[1:]...)
	hh.Info("---start cmd %s with %v ...", app.Name, app.Cmd)

	cmd.Dir = app.Cwd
	env := make(map[string]string)
	for _, e := range os.Environ() {
		i := strings.Index(e, "=")
		env[e[:i]] = e[i+1:]
	}
	if home != "" {
		env["HOME"] = home
	}
	env["TERM"] = "xterm"
	//cmd.Env = append(os.Environ(), "TERM=xterm", fmt.Sprintf("HOME=%s", a.Cwd))
	for k, v := range app.Env {
		if k == "PATH" || k == "LD_LIBRARY_PATH" {
			v = v + ":" + env[k]
		}
		env[k] = v
	}
	Env := []string{}
	for k, v := range env {
		Env = append(Env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = Env
	stdoutWriter, err := os.OpenFile(fmt.Sprintf("%s/%s-0.log", logdir, app.Name),
		os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		hh.Error("failed open stdout file in %s, %v", logdir, err)
		return
	}
	defer stdoutWriter.Close()
	stderrWriter, err := os.OpenFile(fmt.Sprintf("%s/%s-1.log", logdir, app.Name),
		os.O_CREATE|os.O_WRONLY, 0644) //|os.O_APPEND
	if err != nil {
		hh.Error("failed open stderr file in %s, %v", logdir, err)
		return
	}
	defer stderrWriter.Close()

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	err = cmd.Start()
	if err != nil {
		hh.Error("start %s failed %s", app.Name, err)
		return
	}

	p_stat, err := cmd.Process.Wait()
	hh.Warn("%s exited with code: %v err: %v", app.Name, p_stat.ExitCode(), err)
}

type FileWriteCallback func(fpath string) error

func WatchDir(basedir string, callback FileWriteCallback) {
	fw, _ := fsnotify.NewWatcher()
	filepath.Walk(basedir, func(path string, info os.FileInfo, err error) error {
		if info != nil && info.IsDir() {
			path, err := filepath.Abs(path)
			if err != nil {
				log.Printf("Walk filepath:%s err1:%v", path, err)
				return nil
			}
			err = fw.Add(path)
			if err != nil {
				log.Printf("Walk filepath:%s err2:%v", path, err)
			}
			log.Printf("Watching path: %s", path)
		}
		return nil
	})

	chgfiles := make(map[string]int)
	for {
		select {
		case event := <-fw.Events:
			{
				if (event.Op & fsnotify.Create) == fsnotify.Create {
				}

				if (event.Op & fsnotify.Write) == fsnotify.Write {
					////log.Printf("----write event (name:%s) (op:%v)\n", event.Name, event.Op)
					//f.Events <- FSEvent{Path: event.Name, Op: 2}
					chgfiles[event.Name] = 1
				}

				if (event.Op & fsnotify.Remove) == fsnotify.Remove {
				}
			}
		case err := <-fw.Errors:
			{
				log.Printf("File Watch Error: %v", err)
			}
		case <-time.After(time.Second * 1):
			{ // do callback after 1 second
				for k, _ := range chgfiles {
					err := callback(k)
					if err != nil {
						log.Printf("callback on %s failed %v", k, err)
					}
				}
				if len(chgfiles) > 0 { // clean the file list
					chgfiles = make(map[string]int)
				}

			}
		}
	}
}

func loadOnceFile(fpath string) (*App, error) {
	_, err := os.Stat(fpath)
	if err != nil && os.IsNotExist(err) {
		log.Printf("Not Exist ConfigFile:%v\n", err)
		return nil, err
	}
	strJson, err := ioutil.ReadFile(fpath)
	if err != nil {
		log.Printf("ReadFile Error:%v\n", err)
		return nil, err
	}
	jobj := &App{}
	err = json.Unmarshal(strJson, jobj)
	if err != nil {
		log.Printf("json.Unmarshal Error:%v\n", err)
		return nil, err
	}
	return jobj, nil
}

func ExtractTarGz(gzipStream io.Reader, target string) error {
	st, err := os.Stat(target)
	if err != nil && os.IsNotExist(err) {
		err = os.MkdirAll(target, 0755)
		if err != nil {
			log.Printf("Can not create dir of %s, %v", target, err)
			return errors.New("can not create target dir")
		}
	} else if err == nil {
		if !st.IsDir() {
			log.Printf("target [%s] is not a directory", target)
			return errors.New("target dir is not a directory")
		}
	} else {
		log.Printf("Stat target %s failed %v", target, err)
		return errors.New("unknown error of stat target directory")
	}

	//gzipStream, err := os.Open(tgz_file)
	//if err != nil {
	//	logger.Error("open tgz_file %s failed %v", tgz_file, err)
	//	return err
	//}
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		log.Printf("ExtractTarGz: NewReader failed")
		return err
	}

	tarReader := tar.NewReader(uncompressedStream)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("ExtractTarGz: Next() failed: %s", err.Error())
		}

		fpath := filepath.Join(target, header.Name)
		info := header.FileInfo()
		if info.IsDir() {
			if err := os.Mkdir(fpath, 0755); err != nil {
				if !os.IsExist(err) {
					log.Printf("ExtractTarGz: Mkdir() failed: %s", err.Error())
				}
			}
			continue
		}
		if header.Typeflag == tar.TypeSymlink {
			//logger.Info("--link: %s -> %s", header.Name, header.Linkname)
			if _, err := os.Stat(fpath); err == nil {
				os.Remove(fpath)
			}
			os.Symlink(header.Linkname, fpath)
			continue
		}
		file, err := os.OpenFile(fpath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(file, tarReader)
		if err != nil {
			return err
		}

	}
	return nil
}

func DownTgz(url, target string) error {
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}
	// Put content on file
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("download failed! %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("http failed %d\n", resp.StatusCode)
		return errors.New(fmt.Sprintf("http request failed %s", resp.Status))
	}

	return ExtractTarGz(resp.Body, target)
}

/**

测试样例 (iasp76: ~/gyf/libs/g/x):
_app.json
----
{"name":"mdlservice", "args":["bash", "t.sh"],
    "env": {"_PL_NG___":"1"}}

t.sh
----
while true; do sleep 1m; date >>/tmp/tt.log; done

export _STARTUP="http://172.18.231.76:7777/libs/g/x.tgz^/data/x"
./sshserv serve -c /data/gosrc/sshserv/dist -f t.json
*/
func startUpProc(logdir string) {
	// 从环境变量中获取启动脚本位置, 带 http:// 的
	/*
		下载tgz 文件, 然后解压, 然后启动其中的 _app.json
	*/
	startup := os.Getenv("_STARTUP") // 格式: http://???^/somepath
	if startup == "" {
		return
	}

	hh.Info("start with [%s]", startup)
	v := strings.SplitN(startup, "^", 2)
	tgz_url := v[0]
	target_path := v[1]
	if err := DownTgz(tgz_url, target_path); err != nil {
		hh.Error("Download %s failed, %v", tgz_url, err)
		return
	}

	// 找到其中的 _app.json
	_app := ""
	filepath.Walk(target_path, func(path string, info os.FileInfo, err error) error {
		if filepath.Base(path) == "_app.json" {
			fpath, err := filepath.Abs(path)
			if err != nil {
				hh.Error("Walk filepath:%s err1:%v", path, err)
				return nil
			}
			_app = fpath
			return errors.New("break")
		}
		return nil
	})
	hh.Info("-- found _app.json script: %s", _app)

	if _, err := os.Stat(_app); os.IsNotExist(err) {
		hh.Error("ERROR: startup %s script not found", _app)
		return
	}

	jobj, err := loadOnceFile(_app)
	if err != nil {
		hh.Error("load oncedir %s json failed, retry later", _app)
	} else {
		APPDIR := filepath.Dir(_app)
		jobj.Env["_APPDIR"] = APPDIR // 这个用于告知程序 _app.json 的位置
		jobj.Cwd = APPDIR            // 替换掉 _app.json 中的当前位置
		//todo 替换命令行和env中带 {_APPDIR} 的变量
		tag := "{_APPDIR}"
		for k, v := range jobj.Env {
			if strings.Index(v, tag) >= 0 {
				jobj.Env[k] = strings.ReplaceAll(v, tag, APPDIR)
			}
		}
		for i, v := range jobj.Cmd {
			if strings.Index(v, tag) >= 0 {
				jobj.Cmd[i] = strings.ReplaceAll(v, tag, APPDIR)
			}
		}
		go func() {
			for {
				//hh.Info("starting startup app: %s, %v", jobj.Name, jobj.Cmd)
				startApp(jobj, logdir, "")
				time.Sleep(time.Second * 5)
			}
		}()
	}
}

func runOnceCheck() {
	logdir := os.Getenv("LOGDIR")
	if logdir == "" {
		logdir = "/tmp/logs"
		os.Setenv("LOGDIR", logdir)
	}
	if _, err := os.Stat(logdir); os.IsNotExist(err) {
		os.MkdirAll(logdir, 0755)
	}

	startUpProc(logdir) // env: _STARTUP

	oncedir := os.Getenv("_ONCEPROC")
	if oncedir == "" {
		return
	}
	if _, err := os.Stat(oncedir); os.IsNotExist(err) {
		os.MkdirAll(oncedir, 0755)
	}

	filepath.Walk(oncedir, func(path string, info os.FileInfo, err error) error {
		fpath, err := filepath.Abs(path)
		if err != nil {
			log.Printf("Walk filepath:%s err1:%v", path, err)
			return nil
		}
		//log.Printf("---file: %s isdir: %v", fpath, info.IsDir())
		if info != nil && info.IsDir() {
			return nil
		}

		jobj, err := loadOnceFile(fpath)
		if err != nil {
			log.Printf("load oncedir %s json failed, retry later", fpath)
		} else {
			err = os.Remove(fpath)
			if err != nil {
				log.Printf("remove file %s failed %v", fpath, err)
			}

			go startApp(jobj, logdir, "")
		}
		return nil
	})

	WatchDir(oncedir, func(fpath string) error {
		log.Printf("changed file %s", fpath)
		jobj, err := loadOnceFile(fpath)
		if err != nil {
			log.Printf("load oncedir %s json failed, retry later")
		} else {
			err = os.Remove(fpath)
			if err != nil {
				log.Printf("remove file %s failed %v", fpath, err)
			}

			go startApp(jobj, logdir, "")
		}
		return nil
	})
}
