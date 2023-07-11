package cmd

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lulugyf/sshserv/hh"

	//"log"

	"github.com/lulugyf/sshserv/api"
	"github.com/lulugyf/sshserv/config"
	"github.com/lulugyf/sshserv/dataprovider"
	"github.com/lulugyf/sshserv/logger"
	"github.com/lulugyf/sshserv/serv"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	configDirFlag    = "config-dir"
	configDirKey     = "config_dir"
	configFileFlag   = "config-file"
	configFileKey    = "config_file"
	logFilePathFlag  = "log-file-path"
	logFilePathKey   = "log_file_path"
	logMaxSizeFlag   = "log-max-size"
	logMaxSizeKey    = "log_max_size"
	logMaxBackupFlag = "log-max-backups"
	logMaxBackupKey  = "log_max_backups"
	logMaxAgeFlag    = "log-max-age"
	logMaxAgeKey     = "log_max_age"
	logCompressFlag  = "log-compress"
	logCompressKey   = "log_compress"
	logVerboseFlag   = "log-verbose"
	logVerboseKey    = "log_verbose"
)

var (
	configDir     string
	configFile    string
	logFilePath   string
	logMaxSize    int
	logMaxBackups int
	logMaxAge     int
	logCompress   bool
	logVerbose    bool
	testVar       string
	serveCmd      = &cobra.Command{
		Use:   "serve",
		Short: "Start the SSH Server",
		Long: `To start the SSH Server with the default values for the command line flags simply use:

sftpgo serve
		
Please take a look at the usage below to customize the startup options`,
		Run: func(cmd *cobra.Command, args []string) {
			startServe()
		},
	}
)

func init() {
	rootCmd.AddCommand(serveCmd)

	viper.SetDefault(configDirKey, ".")
	viper.BindEnv(configDirKey, "SFTPGO_CONFIG_DIR")
	serveCmd.Flags().StringVarP(&configDir, configDirFlag, "c", viper.GetString(configDirKey),
		"Location for SFTPGo config dir. This directory should contain the \"sftpgo\" configuration file or the configured "+
			"config-file and it is used as the base for files with a relative path (eg. the private keys for the SFTP server, "+
			"the SQLite database if you use SQLite as data provider). This flag can be set using SFTPGO_CONFIG_DIR env var too.")
	viper.BindPFlag(configDirKey, serveCmd.Flags().Lookup(configDirFlag))

	viper.SetDefault(configFileKey, config.DefaultConfigName)
	viper.BindEnv(configFileKey, "SFTPGO_CONFIG_FILE")
	serveCmd.Flags().StringVarP(&configFile, configFileFlag, "f", viper.GetString(configFileKey),
		"Name for SFTPGo configuration file. It must be the name of a file stored in config-dir not the absolute path to the "+
			"configuration file. The specified file name must have no extension we automatically load JSON, YAML, TOML, HCL and "+
			"Java properties. Therefore if you set \"sftpgo\" then \"sftpgo.json\", \"sftpgo.yaml\" and so on are searched. "+
			"This flag can be set using SFTPGO_CONFIG_FILE env var too.")
	viper.BindPFlag(configFileKey, serveCmd.Flags().Lookup(configFileFlag))

	viper.SetDefault(logFilePathKey, "logs/sshserv.log")
	viper.BindEnv(logFilePathKey, "SFTPGO_LOG_FILE_PATH")
	serveCmd.Flags().StringVarP(&logFilePath, logFilePathFlag, "l", viper.GetString(logFilePathKey),
		"Location for the log file. This flag can be set using SFTPGO_LOG_FILE_PATH env var too.")
	viper.BindPFlag(logFilePathKey, serveCmd.Flags().Lookup(logFilePathFlag))

	viper.SetDefault(logMaxSizeKey, 10)
	viper.BindEnv(logMaxSizeKey, "SFTPGO_LOG_MAX_SIZE")
	serveCmd.Flags().IntVarP(&logMaxSize, logMaxSizeFlag, "s", viper.GetInt(logMaxSizeKey),
		"Maximum size in megabytes of the log file before it gets rotated. This flag can be set using SFTPGO_LOG_MAX_SIZE "+
			"env var too.")
	viper.BindPFlag(logMaxSizeKey, serveCmd.Flags().Lookup(logMaxSizeFlag))

	viper.SetDefault(logMaxBackupKey, 5)
	viper.BindEnv(logMaxBackupKey, "SFTPGO_LOG_MAX_BACKUPS")
	serveCmd.Flags().IntVarP(&logMaxBackups, "log-max-backups", "b", viper.GetInt(logMaxBackupKey),
		"Maximum number of old log files to retain. This flag can be set using SFTPGO_LOG_MAX_BACKUPS env var too.")
	viper.BindPFlag(logMaxBackupKey, serveCmd.Flags().Lookup(logMaxBackupFlag))

	viper.SetDefault(logMaxAgeKey, 28)
	viper.BindEnv(logMaxAgeKey, "SFTPGO_LOG_MAX_AGE")
	serveCmd.Flags().IntVarP(&logMaxAge, "log-max-age", "a", viper.GetInt(logMaxAgeKey),
		"Maximum number of days to retain old log files. This flag can be set using SFTPGO_LOG_MAX_AGE env var too.")
	viper.BindPFlag(logMaxAgeKey, serveCmd.Flags().Lookup(logMaxAgeFlag))

	viper.SetDefault(logCompressKey, false)
	viper.BindEnv(logCompressKey, "SFTPGO_LOG_COMPRESS")
	serveCmd.Flags().BoolVarP(&logCompress, logCompressFlag, "z", viper.GetBool(logCompressKey), "Determine if the rotated "+
		"log files should be compressed using gzip. This flag can be set using SFTPGO_LOG_COMPRESS env var too.")
	viper.BindPFlag(logCompressKey, serveCmd.Flags().Lookup(logCompressFlag))

	viper.SetDefault(logVerboseKey, true)
	viper.BindEnv(logVerboseKey, "SFTPGO_LOG_VERBOSE")
	serveCmd.Flags().BoolVarP(&logVerbose, logVerboseFlag, "v", viper.GetBool(logVerboseKey), "Enable verbose logs. "+
		"This flag can be set using SFTPGO_LOG_VERBOSE env var too.")
	viper.BindPFlag(logVerboseKey, serveCmd.Flags().Lookup(logVerboseFlag))
}

func DownFile(url, target string) error {
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}
	// Put content on file
	resp, err := client.Get(url)
	if err != nil {
		hh.Info("error open url %s, %v\n", url, err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		hh.Info("http failed %d\n", resp.StatusCode)
		return errors.New(fmt.Sprintf("http request failed %s", resp.Status))
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		hh.Info("can not open file %s, %v\n", target, err)
		return err
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		hh.Info("download failed %v\n", err)
		return err
	} else {
		hh.Info("down successful with %d bytes\n", n)
	}
	return nil
}

/*
./sshserv serve
./sshserv serve -c /data/app/ss
./sshserv serve -c /data/app/ss -f sshserv.json
./sshserv serve -c /data/app/ss1 -f http://172.18.231.76:7777/libs/g/sshserv_pl.json
*/
func startServe() {

	//f_log, _ := os.OpenFile("sshserv_debug.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	//log.SetOutput(f_log)

	logLevel := zerolog.DebugLevel
	if !logVerbose {
		logLevel = zerolog.InfoLevel
	}
	if configDir != "." {
		if _, err := os.Stat(configDir); os.IsNotExist(err) {
			os.MkdirAll(configDir, 0755)
		}
		os.Chdir(configDir)
	}
	logger.InitLogger(logFilePath, logMaxSize, logMaxBackups, logMaxAge, logCompress, logLevel)

	hhlogfile := "logs/hh.log" // fmt.Sprintf("%s/hh.log", filepath.Dir(logFilePath))
	//hh.InitLogger(hhlogfile)  // move to main.go
	log.Printf("--hhlogfile: %s logFilePath: %s, configDir: %s", hhlogfile, logFilePath, configDir)
	logger.Info(logSender, "starting SFTPGo, config dir: %v, config file: %v, log max size: %v log max backups: %v "+
		"log max age: %v log verbose: %v, log compress: %v", configDir, configFile, logMaxSize, logMaxBackups, logMaxAge,
		logVerbose, logCompress)
	_conf := configFile
	if strings.HasPrefix(_conf, "http://") {
		hh.Info("-- download config %s", _conf)
		_conf = fmt.Sprintf("%s/.conf.json", configDir)
		if err := DownFile(configFile, _conf); err != nil {
			hh.Info("Download conf file %s failed, %v\n", configFile, err)
			os.Exit(1)
		}
		_conf = ".conf.json"
	}
	hh.Info("loading config %s %s", configDir, _conf)
	config.LoadConfig(configDir, _conf)
	//config.LoadConfig(configDir, configFile)
	providerConf := config.GetProviderConf()

	err := dataprovider.Initialize(providerConf, configDir)
	if err != nil {
		logger.Error(logSender, "error initializing data provider: %v", err)
		logger.ErrorToConsole("error initializing data provider: %v", err)
		os.Exit(1)
	}

	dataProvider := dataprovider.GetProvider()
	sshdConf := config.GetSFTPDConfig()
	httpdConf := config.GetHTTPDConfig()

	serv.SetDataProvider(dataProvider)

	logger.Info(logSender, "starting user expire scan...")
	go check_user_expire(dataProvider)

	shutdown := make(chan bool)

	go func() {
		hh.Info("initializing SSHD server with config %+v", sshdConf)
		if err := sshdConf.Initialize(configDir); err != nil {
			//logger.Error(logSender, "could not start SFTP server: %v", err)
			hh.Error("could not start SFTP server: %v", err)
		}
		shutdown <- true
	}()

	if httpdConf.BindPort > 0 {
		router := api.GetHTTPRouter()
		api.SetDataProvider(dataProvider)

		go func() {
			logger.Debug(logSender, "initializing HTTP server with config %+v", httpdConf)
			s := &http.Server{
				Addr:           fmt.Sprintf("%s:%d", httpdConf.BindAddress, httpdConf.BindPort),
				Handler:        router,
				ReadTimeout:    300 * time.Second,
				WriteTimeout:   300 * time.Second,
				MaxHeaderBytes: 1 << 20, // 1MB
			}
			if err := s.ListenAndServe(); err != nil {
				logger.Error(logSender, "could not start HTTP server: %v", err)
				logger.ErrorToConsole("could not start HTTP server: %v", err)
			}
			shutdown <- true
		}()
	} else {
		logger.Debug(logSender, "HTTP server not started, disabled in config file")
		logger.DebugToConsole("HTTP server not started, disabled in config file")
	}

	tconf := httpdConf.TunnelConf
	if tconf != "" {
		if _, err := os.Stat(tconf); err == nil {
			go func() {
				hh.Info("tunnel starting from config %s", tconf)
				WSTunnel(tconf)
				shutdown <- true
			}()
		} else {
			hh.Warn("tunnel conf file (%s) not found", tconf)
		}
	} else {
		hh.Debug("No tunnel conf configured!")
	}

	go runOnceCheck()

	<-shutdown
}

func Serve() {
	startServe()
}

// GetUsers(p Provider, limit int, offset int, order string, username string) ([]User, error)
func check_user_expire_once(provider dataprovider.Provider) {
	var _to_be_deleted []dataprovider.User = make([]dataprovider.User, 0)
	for i := 0; true; i += 30 {
		users, err := dataprovider.GetUsers(provider, 30, i, "", "")
		if err != nil {
			break
		}
		if len(users) == 0 {
			break
		}
		now := time.Now().Unix()
		var ex_tm int64
		for _, user := range users {
			//fmt.Printf("--- checking user: %s\n", user.Username)
			for _, p := range user.Permissions {
				if len(p) < 10 || p[:8] != "_expire:" {
					continue
				}
				fmt.Sscanf(p[8:], "%d", &ex_tm)
				if now > ex_tm {
					//fmt.Printf("user %s expired\n", user.Username)
					_to_be_deleted = append(_to_be_deleted, user)
				}
			}
		}
	}
	//remove the user record
	for _, user := range _to_be_deleted {
		logger.Warn(logSender, "user %s expired, removed!", user.Username)
		dataprovider.DeleteUser(provider, user)
	}

}

func check_user_expire(provider dataprovider.Provider) {
	for {
		check_user_expire_once(provider)
		time.Sleep(60 * time.Second)
	}
}
