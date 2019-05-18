package mapreduce

import (
	"fmt"
	"sync"
)

//
// schedule() starts and waits for all tasks in the given phase (mapPhase
// or reducePhase). the mapFiles argument holds the names of the files that
// are the inputs to the map phase, one per map task. nReduce is the
// number of reduce tasks. the registerChan argument yields a stream
// of registered workers; each item is the worker's RPC address,
// suitable for passing to call(). registerChan will yield all
// existing registered workers (if any) and new ones as they register.
//
func schedule(jobName string, mapFiles []string, nReduce int, phase jobPhase, registerChan chan string) {
	var ntasks int
	var n_other int // number of inputs (for reduce) or outputs (for map)
	switch phase {
	case mapPhase:
		ntasks = len(mapFiles)
		n_other = nReduce
	case reducePhase:
		ntasks = nReduce
		n_other = len(mapFiles)
	}

	fmt.Printf("Schedule: %v %v tasks (%d I/Os)\n", ntasks, phase, n_other)

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

	//=========================================================================================================================
	//map、reduce两部分合一
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

	//=========================================================================================================================
	////一开始分开两部分的写法
	//=========================================================================================================================
	//if phase == mapPhase{
	//	var wg sync.WaitGroup
	//	var taskMutex sync.Mutex //控制失败任务的锁
	//	for{
	//		//mapFiles：每次要完成的任务
	//		//failedTasks：保存失败的任务，下一次完成
	//		failedTasks := make([]string, 0)
	//		for taskNumber, task := range mapFiles{
	//
	//			//注意worker节点只会在一开始注册一次！
	//			//当goroutine完成时，再次通过registerchan注册可用节点
	//			curWork := <- registerChan
	//
	//			wg.Add(1)
	//			//分配任务
	//			//这里必须以函数调用方式传递参数，否则go里面的变量会绑定为taskNumber和task本身，而不是其遍历的值，最后的结果就是多个goroutine运行同一个任务
	//			go func(curWorker string, num int, task string) {
	//				defer wg.Done()
	//				ok := call(curWorker, "Worker.DoTask", DoTaskArgs{jobName, task, phase, num, n_other}, nil)
	//				if !ok{
	//					fmt.Printf("%s phase task %d failed in %s", phase, taskNumber, curWorker)
	//					taskMutex.Lock()
	//					failedTasks = append(failedTasks, task)
	//					taskMutex.Unlock()
	//				}else{
	//					//仅仅写registerChan <- curWorker，最后的几个chan会堵塞，因为主程序已经结束了，管道另一边无法接收数据。
	//					//所以另开一个goroutine防止堵塞
	//					go func() {registerChan <- curWorker}()
	//				}
	//			}(curWork, taskNumber, task)
	//		}
	//
	//		wg.Wait()
	//		remain := len(failedTasks)
	//		fmt.Printf("#%d tasks failed, total: %d\n", remain, ntasks)
	//		//所有工作都成功
	//		if remain == 0 {
	//			break
	//		}
	//
	//		//继续完成未完成的工作
	//		mapFiles = mapFiles[:remain]
	//		copy(mapFiles, failedTasks)
	//	}
	//}else{
	//	//只有nReduce个节点运行。
	//	var wg sync.WaitGroup
	//	var taskMutex sync.Mutex //控制失败任务的锁
	//	toDoTask := make([]int, nReduce)
	//	for i := 0; i < nReduce; i++{
	//		toDoTask = append(toDoTask, i)
	//	}
	//	for{
	//		failedTasks := make([]int, 0)
	//		for _, task := range toDoTask{
	//			curWorker := <- registerChan
	//
	//			wg.Add(1)
	//			go func(curWorker string, taskNumber int) {
	//				defer wg.Done()
	//				ok := call(curWorker, "Worker.DoTask", DoTaskArgs{jobName, "", phase, taskNumber, n_other}, nil)
	//				if !ok{
	//					fmt.Printf("%s phase task %d failed in %s", phase, taskNumber, curWorker)
	//					taskMutex.Lock()
	//					failedTasks = append(failedTasks, taskNumber)
	//					taskMutex.Unlock()
	//				}else{
	//					go func() {registerChan <- curWorker}()
	//				}
	//			}(curWorker, task)
	//		}
	//		wg.Wait()
	//
	//		ntasks = len(failedTasks)
	//		if ntasks == 0{
	//			break
	//		}
	//		toDoTask = toDoTask[:ntasks]
	//		copy(toDoTask, failedTasks)
	//	}
	//}

	fmt.Printf("Schedule: %v done\n", phase)
}
