#	MIT6.824 Lab1-Mapreduce
&emsp;&emsp;MIT6.824是一门非常出名的分布式课程，在这门课里，我们能了解到分布式系统的前生今世，领略一种种令人拍案叫绝的算法，顺便学一下人际管理，有人戏称，分布式系统是一门在你有多个女朋友并且她们都相互认识的情况下，将她们妥善安置的学问，细细一想，觉得很有道理，然而真学完这门课估计就找不到女朋友了，因为那时候已经秃头了...  

&emsp;&emsp;言归正传，我刚上完前两节课程（其实就是看了一篇mapreduce论文和go语言入门），就来做lab1了，收获良多，下面分享一下我做lab1时的一些认识和想法。  

&emsp;&emsp;lab1要求使用Go语言实现mapreduce，包括任务安排、节点调度以及map和reduce的操作和相互之间的配合。事实上，大部分代码已经写好了，我们只需要完成核心的代码就可以了。但是我还是建议大家看看其他的代码，从中可以学到许多东西。包括但不限于如何将问题拆分为几个小问题，如何构建一个微型服务器，如何去在客户和服务之间交互，如何设计相关的数据结构和服务，这些都能在6.824/src/mapreduce/*.go文件里找到。  

#	预备知识
在mapreduce框架下，执行一个job可以大致分为以下几个过程：
1.	由用户确定输入文件的数量，map和reduce函数，以及reduce任务的数量(nReduce)。
2.	序会自动创建一个master节点。master节点首先会开启一个RPC服务器(在master_rpc.go里），然后等待worker注册（使用rpc call Register()，定义在master.go里）。当任务来临时，master决定如何将任务分配给workers，以及如何处理workers failures的情况（schedule()定义在schedule.go里）。
3.	master将一个输入文件看成是一个map任务，并且给每个map任务至少调用一次doMap()（在common_map.go里）。这时候有两种情况，当模式选择的是sequential()时，master节点直接进行map任务；如果使用分布式的模式，那么master节点会通过DoTask的RPC调用将任务分派给workers(在works.go）。每一个对doMap()的调用都会自动读取相应文件，并且对文件里的数据进行预先定义好的map操作，并且最终的key/value结果存放在workers本地的nReduce中间文件中。doMap()会对任务进行hash以决定对应的reduce中间文件。所以一次任务一共会产生nMap*nReduce个中间文件（每个map节点创建nReduce个中间文件）。每个文件名字会包含一个前缀，其中包括map任务的编号，以及reduce任务的编号。例如，如果有两个map任务和3个reduce任务，那么将会创建6个中间文件（一般情况下是reduce节点少于map节点）。
>mrtmp.xxx-0-0  
>mrtmp.xxx-0-1  
>mrtmp.xxx-0-2  
>mrtmp.xxx-1-0  
>mrtmp.xxx-1-1  
>mrtmp.xxx-1-2  
4.	当map节点完成任务并且生成了nReduce个中间文件后，master会给每个reduce任务至少调用一次doReduce()(在common_reduce.go里）函数。同样的，和doMap()一样，会根据sequentil模式和分布式模式里决定直接在master节点运行或者是分配到reduce节点运行。doReduce()会控制第r个reduce节点读取所有map节点的第r个中间文件，并且对读取到的数据进行定义好的reduce操作。最终，一共会产生nReduce个输出文件。
5.	在所有reduce都完成任务之后，master节点会调用mr.merge()(在master_splitmerge.go里）合并nReduce个输出文件。
6.	最后master节点发送shutdown RPC命令给每一个在master这里注册的worker让他们关机，然后关闭master节点中的RPC 服务器。  
	
	注：这个实验只需要改写doMap,doReduce和schedule三个函数，他们分别在common_map.go,common_reduce.go和schedule.go三个文件中。同样的，你需要在../main/wc.go里定义map和reduce函数。你不需要去改动其他函数，但是阅读其他函数有助于帮助你理解代码逻辑，加深对整个系统架构的理解。
	
#	Part1-实现map和reduce节点的核心代码
map节点和reduce节点的核心代码并不是实现map函数和reduce函数，后两者是用户提供的。  
map节点具体的核心代码如下：
1.	读取需要处理的文件。
2.	对读取内容调用客户定义好的map函数。
3.	将map结果输出为nReduce个中间文件，方便reduce节点获取。（用hash函数来确定哪些结果应该存放到那个中间文件中，这样一来不同map节点的相同数据就能映射到同一个reduce节点里处理）  

```go
func doMap(
	jobName string, // the name of the MapReduce job
	mapTask int, // which map task this is
	inFile string,
	nReduce int, // the number of reduce task that will be run ("R" in the paper)
	mapF func(filename string, contents string) []KeyValue,
) {
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
```

reduce节点的工作流程：
1.	从**所有**map节点中读取属于自己的中间文件。
2.	对本节点的key值进行排序，方便后续处理。
3.	对每一个key值调用客户定义好的reduce函数。
4.	将结果输出到共享文件系统。  

```go
func doReduce(
	jobName string, // the name of the whole MapReduce job
	reduceTask int, // which reduce task this is
	outFile string, // write the output here
	nMap int, // the number of map tasks that were run ("M" in the paper)
	reduceF func(key string, values []string) string,
) {
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
		//也可以在之前defer file.Close()
		//不过defer是return时才执行，我想早点关闭文件。
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

```

#	Part2-实现简单的字频统计
没什么好说的，根据提示查一下go如何切分字符串即可。  

```go
func mapF(filename string, contents string) []mapreduce.KeyValue {
	// Your code here (Part II).
	kvpairs := make([]mapreduce.KeyValue, 0)
	f := func(r rune)bool{
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}
	//https://golang.org/pkg/strings/#FieldsFunc
	//将满足string中满足f的去掉，并且以满足f的作为两个word之间的分割线
	words := strings.FieldsFunc(contents, f)

	for _, word := range words{
		kvpairs = append(kvpairs, mapreduce.KeyValue{word, strconv.Itoa(1)})
	}

	return kvpairs
}

func reduceF(key string, values []string) string {
	// Your code here (Part II).
	res := 0
	for _, val := range values{
		valInt, _ := strconv.Atoi(val)
		res = res + valInt
	}
	return strconv.Itoa(res)
}

```

#	Part3&4分布式容错执行mapreduce
##	相关背景
part3&4才是这一个lab的精髓，要求使用RPC通信，调用多个节点执行map操作和reduce操作，难度较大，特别是并行操作环境下有些bug不好找。  
  
&emsp;&emsp;part3需要在master建立服务，然后在每一个worker节点也建立服务，对于master节点而言，worker节点是客户；对于worker而言，master节点是客户。要想多节点调度实现mapreduce,首先要做的，就是在master和每个worker节点建立相应的服务器，用来协调流程，发送指令，传递数据。  
>之前一直以为map节点和reduce节点是两套不同的服务器代码，完成了part3之后我才明白，原来节点的框架代码都是一样的。具体来说，工作节点（不管是map节点还是reduce节点）中负责与master节点交互的部分被抽象出来单独写成一套代码，map/reduce节点只是worker节点执行的任务不同，换句话说，执行map操作的工作节点就叫map节点，执行reduce操作的工作节点就叫reduce节点。如此一来，一个工作节点可能之前是map节点，后面就可以变成reduce节点，大大提升了利用率，是我之前狭隘了。  
首先我们思考一下，客户连接服务器需要哪些信息？第一，当然是得知道服务器的地址；第二，是你所需要调用的服务，在这里也就是要调用的函数；第三，客户提供调用函数所需要的参数；第四，怎么获取服务器的返回信息。  

&emsp;&emsp;相应的，要建立服务器，首先要确定一个地址，其次要确定本服务器所能提供的服务（也就是函数），最后还要确定如何与客户进行信息交互。  
&emsp;&emsp;以上这些都能在master.go和worker.go里面找到，其实只要解决以上几个问题，一个服务器就建立起来了。 

##	认识问题
回到mapreduce，我们可以从功能上将mapreduce拆解为以下几个节点：
1.	 map节点部分：包括读取相应文件，进行map操作，存储中间文件。
2.	reduce节点部分：包括在所有map节点读取相关文件，进行reduce操作，然后合并输出一个文件。
3.	master节点部分：负责分发任务、统筹节点、生成最后的输出结果。

同样的，从任务交互角度出发，程序可以拆分为以下几点：
1.	任务调度：如何将任务分发给相应的节点，以及如何调度不同的工作节点。
2.	状态通知：如何得知各个节点的状态，如何得知节点准备就绪，如何得知任务已经完成。
3.	错误恢复：如果在执行任务时有节点加入如何处理，如果在执行任务时节点失联如何处理，master、map、reduce在处理方式上有什么不同。  

认识到上面几个问题，就能对整个系统有一个宏观的理解和把控。

##	了解程序
通过通读其他代码，我们对程序的运行环境有如下认识：
1.	整个程序是建立在所有节点都运行在一个共享文件系统上的。所以不同节点之间不需要传送文件信息，而只需要传递需要处理的文件名，map和reduce节点通过这个文件名获取相应的数据。所以系统内部只需要传递指令，这些指令由worker和master节点提供的函数构成。
2.	在schedule.go中，描述了master节点和worker节点之间交互的代码，包括建立连接，调用对方的函数，更改变量值，传递指令，结束连接。maste和worker之间通过call来联系，call的第一个参数就是需要联系的服务器名称或者是服务器上需要调用的函数。
3.	正常情况下，worker节点只会注册一次，然后直到接受master的指令才会shutdown，自始至终只会发送一次注册信息，当然如果是节点坏了另说。

##	问题分析
part3&4主要是将多个文件分发到多个节点上，并且要求并行处理。既然是并行处理，那当然是要用goroutine了。这样我们需要解决几个问题：
1.	如何知道哪些节点可用？  

	答：程序中有registerChan管道，专门用来通知可用的节点。
	
2.	如果输入文件大于worker节点数量，也即是一个worker节点可能需要前后运行多个任务，然而worker节点只会注册一次，调度程序如何得知worker已经完成之前的任务了？

	答：worker完成本轮任务后，将本worker再放到registerChan管道里即可，再注册一次。
	
3.	如果因为节点失效导致任务执行失败怎么办？

	答：两种方法，第一个方法是维护一个失败任务的列表（我一开始的想法），运行完一轮后，接着重新运行失败的任务，无限循环，直到任务全部执行成功为止；第二个方法，在开的每个goroutine里执行死循环，直到任务执行成功才跳出（网上的办法，确实是好很多，并且不用加锁）。

以上就是part3&4的核心问题，解决了这几个问题，schedule也就解决了。  

## 程序思路
总的来说，part3&4的思路是这样的：
1.	通过registerChan获取可用的worker节点
2.	通过call()函数给可用的worker节点分发任务
3.	处理失败的任务
4.	使用Sync.WaitGroup等待所有goroutine完成  

第一种方法：
```go
	//=========================================================================================================================
	//统计执行失败的任务编号，在下一个循环中继续执行，直到所有任务都执行完毕。
	//=========================================================================================================================
	// All ntasks tasks have to be scheduled on workers. Once all tasks
	// have completed successfully, schedule() should return.
	//
	// Your code here (Part III, Part IV).
	//
	//
	//var wg sync.WaitGroup //等待所有goroutine完成
	//var taskMutex sync.Mutex //控制失败任务的锁
	//
	////toDoTaskNumber记录需要完成的任务编号
	//toDoTaskNumber := make([]int, ntasks)
	//for i := 0; i < ntasks; i++{
	//	toDoTaskNumber = append(toDoTaskNumber, i)
	//}
	//
	//for {
	//	//toDoTaskNumber：每次要完成任务的编号
	//	//failedTasksNumber：保存失败任务的编号，下一次完成
	//	failedTasksNumber := make([]int, 0)
	//	for _, taskNumber := range toDoTaskNumber {
	//
	//		//注意worker节点只会在一开始注册一次！
	//		//当goroutine完成时，再次通过registerchan注册可用节点
	//		curWork := <-registerChan
	//
	//		wg.Add(1)
	//
	//		var task string
	//		if phase == reducePhase{
	//			task = ""
	//		}else{
	//			task = mapFiles[taskNumber]
	//		}
	//		//分配任务
	//		//这里必须以函数调用方式传递参数，否则go里面的变量会绑定为taskNumber和task本身，而不是其遍历的值，最后的结果就是多个goroutine运行同一个任务
	//		go func(curWorker string, taskNum int, task string) {
	//			defer wg.Done()
	//			ok := call(curWorker, "Worker.DoTask", DoTaskArgs{jobName, task, phase, taskNum, n_other}, nil)
	//			if !ok {
	//				fmt.Printf("%s phase #%d task failed in %s", phase, taskNum, curWorker)
	//				taskMutex.Lock()
	//				failedTasksNumber = append(failedTasksNumber, taskNum)
	//				taskMutex.Unlock()
	//			} else {
	//				//仅仅写registerChan <- curWorker，最后的几个chan会堵塞，因为主程序已经结束了，管道另一边无法接收数据。
	//				//所以另开一个goroutine防止堵塞
	//				go func() { registerChan <- curWorker }()
	//			}
	//		}(curWork, taskNumber, task)
	//	}
	//
	//	wg.Wait()
	//	remain := len(failedTasksNumber)
	//	fmt.Printf("#%d tasks failed, total: %d\n", remain, ntasks)
	//	//所有工作都成功
	//	if remain == 0 {
	//		break
	//	}
	//
	//	//继续完成未完成的工作
	//	toDoTaskNumber = toDoTaskNumber[:remain]
	//	copy(toDoTaskNumber, failedTasksNumber)
	//	}

```

第二种方法：
```go
	//=========================================================================================================================
	//网上的做法
	//在执行任务的goroutine里死循环，直到任务执行完为止
	//=========================================================================================================================
	var wg sync.WaitGroup
	for taskNumber := 0; taskNumber < ntasks; taskNumber++{
		var task string
		if phase == mapPhase{
			task = mapFiles[taskNumber]
		}else{
			task = ""
		}
		taskArgs := DoTaskArgs{jobName, task, phase, taskNumber, n_other}

		wg.Add(1)
		go func(taskArg DoTaskArgs) {
			defer wg.Done()
			for{
				worker := <- registerChan
				ok := call(worker, "Worker.DoTask", taskArgs, nil)
				if ok{
					//开goroutine防止最后几个执行任务的协程阻塞（因为已经没有其他协程接收管道了）
					go func() {
						registerChan <- worker
					}()
					break
				}
			}
		}(taskArgs)
	}
	wg.Wait()
```

##	坑
做的时候遇到了几个坑，分享一下。
1.	在part3 test的时候，会提示缺少pbservice,viewservice等包，这是前几年大作业用到的包，现在（似乎）没有用了。进入6.824/src/main,将diskvd.go,pbc.go,pbd.go,viewd.go删除，或者移到其他文件夹就好。（至少移除后lab1可以正常操作，lab2以后需不需要暂时未知）
2.	假设有这么一段程序
```go
Var wg sync.waitgroup
For xxx{
	Reg := <- chan
	Wg.add(1)
	Go func(){
		defer wg.done()
		//do something
		Chan <- good
	}()
}
Wg.wait()
```

`问题描述`：假设在2个节点上运行20个map任务，你会发现，20个任务完成了，但是卡住了。  
&emsp;&emsp;经过print大法，你发现原来是最后两个goroutine卡住了，为什么呢？  
&emsp;&emsp;经过仔细思考，你发现原来是因为最后两个节点上分别各运行着一个goroutine，而这两个goroutine里都有一个管道阻塞了。  
&emsp;&emsp;这时候问题就出来了。当你执行到最后两个任务时，主程序早已经结束了for循环，但是goroutine里被管道阻塞了，而管道的另一头却在for循环里。    
&emsp;&emsp;也就是说，此时管道只有进，没有出，当然卡住了。
	
`解决方法`：在主程序拿一个数组保存chan的值，但是这时候问题又来了，你怎么知道最后一共有多少个goroutine阻塞？而且这样只不过是从管道发送阻塞变成管道接收阻塞而已。  
&emsp;&emsp;念头一转，你觉得直接chan.close()也许可行？可惜也不可以，同样的，我们也不知道goroutine什么时候结束，管道关早了同样会阻塞。  
&emsp;&emsp;这时候我们重新梳理下我们的需求：  
&emsp;&emsp;我们希望goroutine里的管道不会阻塞当前的goroutine，并且管道的值可能以后还会用到，最好在后续接收前是阻塞的，等待有需要的代码提取管道里的值，而不是单纯的丢弃。  
&emsp;&emsp;没错，你灵光一闪！发现再开一个goroutine不就好了吗。  
&emsp;&emsp;Go func(){chan <- good}()，完美解决问题。  

`事后总结`：这个开goroutine防止阻塞的方法还是我在看master.go里学到的，多看看人家的代码还是有好处的。以后遇到可能会阻塞的东西，特别是包含管道这种说不清道不明的东西，还是另开一个goroutine吧。  
&emsp;&emsp;Part3&4第二种方法在思维上实在比第一种方法要好太多。第一种方法其实是没有理清程序的功能。我们再来回顾一下这段代码，这段代码有两个功能，第一分发任务，第二完成任务。由于完成任务是单独开一个goroutine完成的，所以主程序实际上只有一个功能，那就是分发任务，至于任务如何完成？是否成功完成？任务失败如何处理？这些都应该是属于处理任务的goroutine的功能，而不应该放在主程序里。第二种方法实际上将这段代码进一步抽象成分发任务和处理任务两个模块，并且明确了每个模块的功能，保证每个模块能完成应尽的任务，而绝不跨模块完成不属于自己功能，层次分明，五星好评。这种分解问题，划分模块，明确功能的思维以后要好好训练一下才行。  

#	part5-统计一个单词出现在多少个文件里
也没什么好说的。
```go

func inArray(arr []mapreduce.KeyValue, ele string)bool{
	if len(arr) == 0{
		return false
	}
	for _, word := range arr{
		if word.Key == ele{
			return true
		}

	}
	return false
}
// The mapping function is called once for each piece of the input.
// In this framework, the key is the name of the file that is being processed,
// and the value is the file's contents. The return value should be a slice of
// key/value pairs, each represented by a mapreduce.KeyValue.
func mapF(document string, value string) (res []mapreduce.KeyValue) {
	// Your code here (Part V).
	f := func(r rune)bool{
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}
	//https://golang.org/pkg/strings/#FieldsFunc
	//将满足string中满足f的去掉，并且以满足f的作为两个word之间的分割线
	words := strings.FieldsFunc(value, f)
	for _, word := range words{
		if !inArray(res, word){
			res = append(res, mapreduce.KeyValue{word, document})
		}
	}
	return
}

// The reduce function is called once for each key generated by Map, with a
// list of that key's string value (merged across all inputs). The return value
// should be a single output value for that key.
func reduceF(key string, values []string) string {
	// Your code here (Part V).
	sort.Strings(values)
	res := strconv.Itoa(len(values))+" "+values[0]
	for i := 1; i < len(values); i++{
		res = res + "," + values[i]
	}
	return res
}
```




