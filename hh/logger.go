package hh

import (
	go_logger "github.com/phachon/go-logger"
	"sync"
)

type WriterFunc func(string, ...interface{})

var (
	_logger *go_logger.Logger = nil
	lock                      = &sync.Mutex{}

	Info  WriterFunc
	Error WriterFunc
	Warn  WriterFunc
	Debug WriterFunc
)

func _initLogger(logfile string) {
	_logger = go_logger.NewLogger()
	//fmt.Printf("logger %v\n", logger)

	_logger.Detach("console")

	// console adapter config
	consoleConfig := &go_logger.ConsoleConfig{
		Color:      true,                                                       // Does the text display the color
		JsonFormat: false,                                                      // Whether or not formatted into a JSON string
		Format:     "%timestamp_format% [%level_string%] %file%:%line% %body%", // JsonFormat is false, logger message output to console format string
	}
	// add output to the console
	_logger.Attach("console", go_logger.LOGGER_LEVEL_DEBUG, consoleConfig)

	if logfile != "" {
		// file adapter config
		fileConfig := &go_logger.FileConfig{
			Filename: logfile, // The file name of the logger output, does not exist automatically
			// If you want to separate separate logs into files, configure LevelFileName parameters.
			//LevelFileName: map[int]string{
			//	logger.LoggerLevel("error"): "./error.log", // The error level log is written to the error.log file.
			//	logger.LoggerLevel("info"):  "./info.log",  // The info level log is written to the info.log file.
			//	logger.LoggerLevel("debug"): "./debug.log", // The debug level log is written to the debug.log file.
			//},
			MaxSize:    1024 * 1024,                                  // File maximum (KB), default 0 is not limited
			MaxLine:    100000,                                       // The maximum number of lines in the file, the default 0 is not limited
			DateSlice:  "d",                                          // Cut the document by date, support "Y" (year), "m" (month), "d" (day), "H" (hour), default "no".
			JsonFormat: false,                                        // Whether the file data is written to JSON formatting
			Format:     "%timestamp_format% [%level_string%] %body%", // JsonFormat is false, logger message written to file format string
		}
		// add output to the file
		_logger.Attach("file", go_logger.LOGGER_LEVEL_DEBUG, fileConfig)
	}

	Info = _logger.Infof
	Error = _logger.Errorf
	Debug = _logger.Debugf
	Warn = _logger.Warningf
}

func InitLogger(logfile string) {
	if _logger == nil {
		lock.Lock()
		defer lock.Unlock()
		_initLogger(logfile)
	} else {
		lock.Lock()
		defer lock.Unlock()
		_logger.Detach("console")
		_logger.Detach("file")
		_initLogger(logfile)
	}
}

//func write(w WriterFunc, format string, v ...interface{}) {
//	msg := fmt.Sprintf(format, v...)
//	_, file, line, ok := runtime.Caller(2)
//	if !ok {
//		file = "null"
//		line = 0
//	} else {
//		//funcName = runtime.FuncForPC(pc).Name()
//	}
//	_, filename := path.Split(file)
//	w("%s:%d %s", filename, line, msg)
//}

//func Info(format string, v ...interface{}) {
//	write(logger.Infof, format, v...)
//}
//
//func Debug(format string, v ...interface{}) {
//	//logger.Debugf(format, v...)
//	write(logger.Debugf, format, v...)
//}
//
//func Warn(format string, v ...interface{}) {
//	//logger.Warningf(format, v...)
//	write(logger.Warningf, format, v...)
//}
//
//func Error(format string, v ...interface{}) {
//	//logger.Errorf(format, v...)
//	write(logger.Errorf, format, v...)
//}
