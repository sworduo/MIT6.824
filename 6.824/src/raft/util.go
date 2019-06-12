package raft

import "log"
import "os"
import "io"

// Debugging
const Debug = 1

var (
	InfoRaft *log.Logger
	WarnRaft *log.Logger

	InfoKV *log.Logger //lab3 log
)

//初始化log
func init(){
	infoFile, err := os.OpenFile("infoRaft.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil{
		log.Fatalln("Open infoFile failed.\n", err)
	}
	warnFile, err := os.OpenFile("warnRaft.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil{
		log.Fatalln("Open warnFile failed.\n", err)
	}

	InfoKVFile, err := os.OpenFile("infoKV.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil{
		log.Fatalln("Open infoKVFile failed.\n", err)
	}
	//log.Lshortfile打印出错的函数位置
	InfoRaft = log.New(io.MultiWriter(os.Stderr, infoFile), "InfoRaft:", log.Ldate | log.Ltime | log.Lshortfile)
	WarnRaft = log.New(io.MultiWriter(os.Stderr, warnFile), "WarnRaft:", log.Ldate | log.Ltime | log.Lshortfile)
	InfoKV = log.New(io.MultiWriter(os.Stderr, InfoKVFile), "InfoKV:", log.Ldate | log.Ltime | log.Lshortfile)
}

func DPrintf(show bool, level string, format string, a ...interface{}) (n int, err error) {
	if Debug == 0{
		return
	}
	if show == false{
		//是否打印当前raft实例的log
		return
	}

	if level == "info"{
		InfoRaft.Printf(format, a...)
	}else if level == "warn"{
		WarnRaft.Printf(format, a...)
	}
	return
}
