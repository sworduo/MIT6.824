package mapreduce

import (
	"encoding/json"
	"hash/fnv"
	"io/ioutil"
	"os"
)

func doMap(
	jobName string, // the name of the MapReduce job
	mapTask int, // which map task this is
	inFile string,
	nReduce int, // the number of reduce task that will be run ("R" in the paper)
	mapF func(filename string, contents string) []KeyValue,
) {
	//
	// doMap manages one map task: it should read one of the input files
	// (inFile), call the user-defined map function (mapF) for that file's
	// contents, and partition mapF's output into nReduce intermediate files.
	//
	// There is one intermediate file per reduce task. The file name
	// includes both the map task number and the reduce task number. Use
	// the filename generated by reduceName(jobName, mapTask, r)
	// as the intermediate file for reduce task r. Call ihash() (see
	// below) on each key, mod nReduce, to pick r for a key/value pair.
	//
	// mapF() is the map function provided by the application. The first
	// argument should be the input file name, though the map function
	// typically ignores it. The second argument should be the entire
	// input file contents. mapF() returns a slice containing the
	// key/value pairs for reduce; see common.go for the definition of
	// KeyValue.
	//
	// Look at Go's ioutil and os packages for functions to read
	// and write files.
	//
	// Coming up with a scheme for how to format the key/value pairs on
	// disk can be tricky, especially when taking into account that both
	// keys and values could contain newlines, quotes, and any other
	// character you can think of.
	//
	// One format often used for serializing data to a byte stream that the
	// other end can correctly reconstruct is JSON. You are not required to
	// use JSON, but as the output of the reduce tasks *must* be JSON,
	// familiarizing yourself with it here may prove useful. You can write
	// out a data structure as a JSON string to a file using the commented
	// code below. The corresponding decoding functions can be found in
	// common_reduce.go.
	//
	//   enc := json.NewEncoder(file)
	//   for _, kv := ... {
	//     err := enc.Encode(&kv)
	//
	// Remember to close the file after you have written all the values!
	//
	// Your code here (Part I).
	//

	//读入文件中的数据
	//此处应有处理读取失败的处理，不过刚学go，不太熟悉，就不检查了。
	fileContent, _ := ioutil.ReadFile(inFile)
	KVpairs := mapF(inFile, string(fileContent))

	//保存到本地，两个问题
	//第一，需要知道保存的文件名
	//文件名可以通过reduceName(jobName, mapTask, r)获得
	//第二，关键是以什么样的格式保存到本地
	//上面有提示，虽然不需要使用json格式保存到本地，但是因为reduce输出的是json
	//所以这里也使用json格式熟悉一下。

	mapFiles := make(map[string]*json.Encoder)
	for n:=0; n < nReduce; n++{
		filename := reduceName(jobName, mapTask, n)
		file, _ := os.Create(filename)
		//return之后才进行defer之后的语句
		defer file.Close()
		mapFiles[filename] = json.NewEncoder(file)
	}

	for _, kv := range KVpairs {
		kvfile := reduceName(jobName, mapTask, ihash(kv.Key)%nReduce)
		mapFiles[kvfile].Encode(&kv)
	}


}

func ihash(s string) int {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int(h.Sum32() & 0x7fffffff)
}

