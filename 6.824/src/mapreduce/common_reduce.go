package mapreduce

import (
	"encoding/json"
	"os"
	"sort"
)


func doReduce(
	jobName string, // the name of the whole MapReduce job
	reduceTask int, // which reduce task this is
	outFile string, // write the output here
	nMap int, // the number of map tasks that were run ("M" in the paper)
	reduceF func(key string, values []string) string,
) {
	//
	// doReduce manages one reduce task: it should read the intermediate
	// files for the task, sort the intermediate key/value pairs by key,
	// call the user-defined reduce function (reduceF) for each key, and
	// write reduceF's output to disk.
	//
	// You'll need to read one intermediate file from each map task;
	// reduceName(jobName, m, reduceTask) yields the file
	// name from map task m.
	//
	// Your doMap() encoded the key/value pairs in the intermediate
	// files, so you will need to decode them. If you used JSON, you can
	// read and decode by creating a decoder and repeatedly calling
	// .Decode(&kv) on it until it returns an error.
	//
	// You may find the first example in the golang sort package
	// documentation useful.
	//
	// reduceF() is the application's reduce function. You should
	// call it once per distinct key, with a slice of all the values
	// for that key. reduceF() returns the reduced value for that key.
	//
	// You should write the reduce output as JSON encoded KeyValue
	// objects to the file named outFile. We require you to use JSON
	// because that is what the merger than combines the output
	// from all the reduce tasks expects. There is nothing special about
	// JSON -- it is just the marshalling format we chose to use. Your
	// output code will look something like this:
	//
	// enc := json.NewEncoder(file)
	// for key := ... {
	// 	enc.Encode(KeyValue{key, reduceF(...)})
	// }
	// file.Close()
	//
	// Your code here (Part I).
	//

	//string:[]string => key值:对应的value数组
	kStingPairs := make(map[string][]string)
	//每个key值保存一次
	keySlice := make([]string, 0)
	
	//读取m个map节点对应的中间文件
	for i := 0; i < nMap; i++{
		file, _ := os.Open(reduceName(jobName, i, reduceTask))
		enc := json.NewDecoder(file)
		for {
			var kv KeyValue
			err := enc.Decode(&kv)
			if err != nil{
				break
			}
			_, ok := kStingPairs[kv.Key]
			if !ok{
				//如果字符串是第一次遍历，那么初始化其切片。
				//切片append前需要初始化
				kStingPairs[kv.Key] = make([]string, 0)
				keySlice = append(keySlice, kv.Key)
			}
			kStingPairs[kv.Key] = append(kStingPairs[kv.Key], kv.Value)
		}
		file.Close()
	}
	
	//对key值排序，使得输出是有序的
	sort.Strings(keySlice)
	mergeSlice := make([]KeyValue, 0)

	//对每个key进行reduce操作
	for _, key := range keySlice{
		res := reduceF(key, kStingPairs[key])
		mergeSlice = append(mergeSlice, KeyValue{key, res})
	}

	//存结果
	file, _ := os.Create(outFile)
	defer file.Close()
	env := json.NewEncoder(file)
	for _, kv := range mergeSlice{
		env.Encode(&kv)
	}

}
