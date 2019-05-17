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

	// All ntasks tasks have to be scheduled on workers. Once all tasks
	// have completed successfully, schedule() should return.
	//
	// Your code here (Part III, Part IV).
	//

	if phase == mapPhase{
		var wg sync.WaitGroup
		var taskMutex sync.Mutex //控制失败任务的锁
		for{
			//mapFiles：每次要完成的任务
			//failedTasks：保存失败的任务，下一次完成
			failedTasks := make([]string, 0)
			for taskNumber, task := range mapFiles{

				//注意worker节点只会在一开始注册一次！
				//当goroutine完成时，再次通过registerchan注册可用节点
				curWork := <- registerChan

				wg.Add(1)
				//分配任务
				//这里必须以函数调用方式传递参数，否则go里面的变量会绑定为taskNumber和task本身，而不是其遍历的值，最后的结果就是多个goroutine运行同一个任务
				go func(curWorker string, num int, task string) {
					defer wg.Done()
					ok := call(curWorker, "Worker.DoTask", DoTaskArgs{jobName, task, phase, num, n_other}, nil)
					if !ok{
						fmt.Printf("%s phase task %d failed in %s", phase, taskNumber, curWorker)
						taskMutex.Lock()
						failedTasks = append(failedTasks, task)
						taskMutex.Unlock()
					}else{
						//仅仅写registerChan <- curWorker，最后的几个chan会堵塞，因为主程序已经结束了，管道另一边无法接收数据。
						//所以另开一个goroutine防止堵塞
						go func() {registerChan <- curWorker}()
					}
				}(curWork, taskNumber, task)
			}

			wg.Wait()
			remain := len(failedTasks)
			fmt.Printf("#%d tasks failed, total: %d\n", remain, ntasks)
			//所有工作都成功
			if remain == 0 {
				break
			}

			//继续完成未完成的工作
			mapFiles = mapFiles[:remain]
			copy(mapFiles, failedTasks)
		}
	}else{
		//只有nReduce个节点运行。
		var wg sync.WaitGroup
		var taskMutex sync.Mutex //控制失败任务的锁
		toDoTask := make([]int, nReduce)
		for i := 1; i <= nReduce; i++{
			toDoTask = append(toDoTask, i)
		}
		for{
			failedTasks := make([]int, 0)
			for _, task := range toDoTask{
				curWorker := <- registerChan

				wg.Add(1)
				go func(curWorker string, taskNumber int) {
					defer wg.Done()
					ok := call(curWorker, "Worker.DoTask", DoTaskArgs{jobName, "", phase, taskNumber, n_other}, nil)
					if !ok{
						fmt.Printf("%s phase task %d failed in %s", phase, taskNumber, curWorker)
						taskMutex.Lock()
						failedTasks = append(failedTasks, taskNumber)
						taskMutex.Unlock()
					}else{
						go func() {registerChan <- curWorker}()
					}
				}(curWorker, task)
			}
			wg.Wait()

			ntasks = len(failedTasks)
			if ntasks == 0{
				break
			}
			toDoTask = toDoTask[:ntasks]
			copy(toDoTask, failedTasks)
		}
	}

	fmt.Printf("Schedule: %v done\n", phase)
}
