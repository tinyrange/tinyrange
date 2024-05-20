package glog

type Logger bool

func (l Logger) Info(val ...any) {

}

func (l Logger) Infof(fmt string, args ...any) {

}

func Info(val ...any) {

}

func Infof(fmt string, args ...any) {

}

func Warning(val ...any) {

}

func Warningf(fmt string, args ...any) {

}

func Errorf(fmt string, args ...any) {

}

func V(verbose int) Logger {
	return Logger(false)
}
