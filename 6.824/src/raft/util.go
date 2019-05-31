package raft

import "log"
import "os"
import "io"

// Debugging
const Debug = 1

var (
	Info *log.Logger
	Warn *log.Logger
	Error *log.Logger
)

//初始化log
func init(){
	infoFile, err := os.OpenFile("info.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil{
		log.Fatalln("Open infoFile failed.\n", err)
	}
	warnFile, err := os.OpenFile("warn.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil{
		log.Fatalln("Open warnFile failed.\n", err)
	}
	errFile, err := os.OpenFile("err.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil{
		log.Fatalln("Open warnFile failed.\n", err)
	}
	//log.Lshortfile打印出错的函数位置
	Info = log.New(io.MultiWriter(os.Stderr, infoFile), "Info:", log.Ldate | log.Lmicroseconds | log.Lshortfile)
	Warn = log.New(io.MultiWriter(os.Stderr, warnFile), "Warn:", log.Ldate | log.Lmicroseconds | log.Lshortfile)
	Error = log.New(io.MultiWriter(os.Stderr, errFile), "Error:", log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

func DPrintf(show bool, name string, format string, a ...interface{}) (n int, err error) {
	if Debug == 0{
		return
	}
	if show == false{
		//是否打印当前raft实例的log
		return
	}

	if name == "info"{
		Info.Printf(format, a...)
	}else if name == "warn"{
		Warn.Printf(format, a...)
	}else{
		Error.Fatalln("log error!")
	}
	return
}
