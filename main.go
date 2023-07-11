// Full featured and highly configurable SFTP server.
// For more details about features, installation, configuration and usage please refer to the README inside the source tree:
// https://github.com/drakkan/   sftpgo/blob/master/README.md
package main

// done hdfs hostname -> ip
// done sftp + hdfs: ls -l show username and groupname
// done sftp + disk: ls -l show username groupname

/*
alias ss="../sshserv serve &"
alias cc="sftp -P 2022 caro@localhost"
*/

import (
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/lulugyf/sshserv/cmd"
	"github.com/lulugyf/sshserv/hh"
	_ "github.com/mattn/go-sqlite3"
	"os/exec"
	"time"

	//	_ "github.com/colinmarc/hdfs"
	"log"
	"os"
	"path/filepath"
)

func main() {
	//cmd.Execute()

	cur_dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}
	os.Chdir(cur_dir) // 跳转到程序文件所在的目录
	logdir := "logs"
	if _, err := os.Stat(logdir); err != nil {
		os.MkdirAll(logdir, 0755)
	}

	hhlogfile := "logs/hh.log" // fmt.Sprintf("%s/hh.log", filepath.Dir(logFilePath))
	hh.InitLogger(hhlogfile)

	// 添加进程监控， 工作进程退出后30秒自动重启
	if len(os.Args) > 1 && os.Args[1] == "wait" {
		for {
			cmd := exec.Command(fmt.Sprintf("%s/%s", cur_dir, os.Args[0]))
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				hh.Error("run failed: %v", err)
			}
			//hh.Info("subprocess started, waiting...")
			//err = cmd.Wait()
			//hh.Info("wait return %v", err)
			time.Sleep(time.Second * 30)
		}
		return
	}

	cmd.Serve()
}
