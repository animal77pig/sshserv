// Package config manages the configuration.
// Configuration is loaded from sftpgo.conf file.
// If sftpgo.conf is not found or cannot be readed or decoded as json the default configuration is used.
// The default configuration an be found inside the source tree:
// https://github.com/drakkan/   sftpgo/blob/master/sftpgo.conf
package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/lulugyf/sshserv/hh"

	"github.com/lulugyf/sshserv/api"
	"github.com/lulugyf/sshserv/dataprovider"
	"github.com/lulugyf/sshserv/logger"
	"github.com/lulugyf/sshserv/serv"
	"github.com/spf13/viper"
)

const (
	logSender     = "config"
	defaultBanner = "SSHServ"
	// DefaultConfigName defines the name for the default config file.
	// This is the file name without extension, we use viper and so we
	// support all the config files format supported by viper
	DefaultConfigName = "sshserv"
	// ConfigEnvPrefix defines a prefix that ENVIRONMENT variables will use
	configEnvPrefix = "sshserv"
)

var (
	globalConf globalConfig
)

type globalConfig struct {
	SSHD         serv.Configuration  `json:"sftpd" mapstructure:"sftpd"`
	ProviderConf dataprovider.Config `json:"data_provider" mapstructure:"data_provider"`
	HTTPDConfig  api.HTTPDConf       `json:"httpd" mapstructure:"httpd"`
}

func init() {
	// create a default configuration to use if no config file is provided
	globalConf = globalConfig{
		SSHD: serv.Configuration{
			Banner:       defaultBanner,
			BindPort:     2022,
			BindAddress:  "",
			IdleTimeout:  15,
			MaxAuthTries: 0,
			Umask:        "0022",
			UploadMode:   0,
			Actions: serv.Actions{
				ExecuteOn:           []string{},
				Command:             "",
				HTTPNotificationURL: "",
			},
			Keys:         []serv.Key{},
			IsSCPEnabled: false,
			FullFunc:     false,
			Ext:          &serv.ExtConf{},
		},
		ProviderConf: dataprovider.Config{
			Driver:           "sqlite",
			Name:             "sftpgo.db",
			Host:             "",
			Port:             5432,
			Username:         "",
			Password:         "",
			ConnectionString: "",
			UsersTable:       "users",
			ManageUsers:      1,
			SSLMode:          0,
			TrackQuota:       1,
		},
		HTTPDConfig: api.HTTPDConf{
			BindPort:    8080,
			BindAddress: "127.0.0.1",
			TunnelConf:  "",
		},
	}

	viper.SetEnvPrefix(configEnvPrefix)
	replacer := strings.NewReplacer(".", "__")
	viper.SetEnvKeyReplacer(replacer)
	viper.SetConfigName(DefaultConfigName)
	setViperAdditionalConfigPaths()
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
}

// GetSFTPDConfig returns the configuration for the SFTP server
func GetSFTPDConfig() serv.Configuration {
	return globalConf.SSHD
}

// GetHTTPDConfig returns the configuration for the HTTP server
func GetHTTPDConfig() api.HTTPDConf {
	return globalConf.HTTPDConfig
}

// GetProviderConf returns the configuration for the data provider
func GetProviderConf() dataprovider.Config {
	return globalConf.ProviderConf
}

func _LoadConfig(fpath string, cc *globalConfig) error {
	if !strings.HasSuffix(fpath, ".json") {
		fpath = fpath + ".json"
	}
	_, err := os.Stat(fpath)
	if err != nil && os.IsNotExist(err) {
		log.Printf("Not Exist ConfigFile:%v\n", err)
		return err
	}
	configJson, err := ioutil.ReadFile(fpath)
	if err != nil {
		log.Printf("ReadFile Error:%v\n", err)
		return err
	}

	hh.Warn("--config fpath: %s", fpath)
	err = json.Unmarshal(configJson, cc)
	if err != nil {
		log.Printf("json.Unmarshal Error:%v\n", err)
		return err
	}
	return nil
}

// LoadConfig loads the configuration
// configDir will be added to the configuration search paths.
// The search path contains by default the current directory and on linux it contains
// $HOME/.config/sftpgo and /etc/sftpgo too.
// configName is the name of the configuration to search without extension
func LoadConfig(configDir, configName string) error {
	var err error
	if err = _LoadConfig(configName, &globalConf); err != nil {
		viper.AddConfigPath(configDir)
		viper.SetConfigName(configName)
		if err = viper.ReadInConfig(); err != nil {
			logger.Warn(logSender, "error loading configuration file: %v. Default configuration will be used: %+v", err, globalConf)
			logger.WarnToConsole("error loading configuration file: %v. Default configuration will be used.", err)
			return err
		}
		err = viper.Unmarshal(&globalConf)
		if err != nil {
			logger.Warn(logSender, "error parsing configuration file: %v. Default configuration will be used: %+v", err, globalConf)
			logger.WarnToConsole("error parsing configuration file: %v. Default configuration will be used.", err)
			return err
		}
	}
	if strings.TrimSpace(globalConf.SSHD.Banner) == "" {
		globalConf.SSHD.Banner = defaultBanner
	}
	if globalConf.SSHD.UploadMode < 0 || globalConf.SSHD.UploadMode > 1 {
		err = fmt.Errorf("Invalid upload_mode 0 and 1 are supported, configured: %v reset upload_mode to 0",
			globalConf.SSHD.UploadMode)
		globalConf.SSHD.UploadMode = 0
		logger.Warn(logSender, "Configuration error: %v", err)
		logger.WarnToConsole("Configuration error: %v", err)
	}
	logger.Debug(logSender, "config file used: '%v', config loaded: %+v", viper.ConfigFileUsed(), globalConf)
	return err
}
