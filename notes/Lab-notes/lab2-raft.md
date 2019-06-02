#	MIT6.824 Lab2-Raft
&emsp;&emsp;MIT6.824第二个实验，实现著名的分布式一致性算法Raft，包括leader选举、日志复制和状态持久化，不包括成员变更。下面仔细分析一下实验要求和我的解法。  

#	lab2A-Leader election
##	概览
&emsp;&emsp;2A部分要求实现leader选举，共有两个test，一个是集群启动时选出leader，一个是在leader网络分区后，剩下的大多数节点能选举出新的leader。我先自己实现了一遍，通过了测试，然而感觉代码很糟。参考了网上一些解题思路，重构了一遍代码，最后发现每个人的思路都有一种惯性，重构之后发现代码和重构之前差不多。与其说是重构代码，不如说是梳理一遍思路。第一次实现的时候完全就是拆东墙补西墙，对代码逻辑缺乏整体的认识，而重构的时候思路如海飞丝一样丝滑顺畅，可以清晰的把握函数的跳转，状态的变迁，代码的走向，对leader election会有一种更宏观的理解。建议大家第一次实现之后重构一下，花费的时间也不会很多，但是收获觉得不少。  

##	实验要求
1.	在raft.go里完善requestVoteArgs和RequestVoteReply两个数据结构。
2.	完成Make()创建raft实例，和RequestVote请求投票两个方法。
3.	定义一个AppendEntries rpc struct，完成AppendEntries 的 Rpc调用。
4.	为entries定义一个struct

##	实验提示
1.	心跳信号周期要求一秒10次。
2.	要求在5秒内完成一次leader election，包含检测到leader失效、多次选举的时间。
3.	election timeout时间应该大于150ms~300ms，但是也要保证能在5s内完成选举。（注：150ms~300ms是建立在心跳信号在150ms内发送多次的情况，而这里心跳信号100ms才发送一次。）
4。	记住，只要大写的struct和field才能在不同文件之间调用。所以RPC调用的struct必须是大写的。
5.	打log有好处，方便debug，util.go里的DPrintf非常有用。
6.	投票和日志复制要放在单独一个goroutine里，因为程序只需要绝大多数follower回复即可，不需要等待所有follower的回复。
7.	Log entries从1开始，commitIndex 和 lastApplied从0开始，方便根据PrelogIndex同步leader和follower的日志。
8.	新的leader通过appendEntries来告诉和他一起竞争的candidate，他成为新leader了。
9.	日志里的内容command是int型。

##	实验分析
&emsp;&emsp;首先分享重构之后的代码思路。  
&emsp;&emsp;在分析代码之前，首先要明确整个测试环境的流程。只有明白是系统怎么测试的，才能在错误中找到正确的解决方向。整体的运行逻辑如下：
1.	设立一个控制整个网络流的net server，调度raft之间的信息交流、日志复制，同时管束所有的流量，模拟message到不了、丢失、延迟、返回丢失、机器宕机等情况。
2.	clientEnd可以看成是一个个在集群中可用的机器节点，方便net server管制。
3.	在每个节点上创建raft对象，每个raft对象可以是leader,candidate,follower三种身份之一。并且rpc调用，调用的是raft对象里的方法。Net server负责传递信息，clientEnd负责发送信息和接收信息。从网络协议来看，raft是应用层，clientEnd是传输层，net 是网络层。
4.	整个流程：先创立net server，然后建立一个个clientEnd，相当于是启动机器，然后创建 raft对象，相当于是在每个机器上创建raft实例，然后根据raft的field和method，模拟raft分布式一致性算法。所以一个raft实例在创立时，就知道其他peer，也就是其他机器（clientEnd）的地址是什么了。

##	Leader
&emsp;&emsp;整个实验最重要的规划好每个角色的流程，包括何时成为这个角色？成为这个角色后需要做什么？何时从这个角色切换到其他角色？切换的条件是什么？仔细理清楚每个角色在raft中发挥的作用，是实现这个算法的关键。  
&emsp;&emsp;就lab2A，对于leader而言，其逻辑大致如下：
*	只有在一轮选举出，获得超半数票数的candidate才能成为新的leader。
*	按照心跳周期（这里要求一秒发十次），给所有节点发出心跳信息。
*	接到更高term leader的心跳信息，转为follower。
*	append返回的term高于自己的term，转为follower。  

由于是看完论文才写实验，所以在实现2A时，难免会实现一些日志复制的内容，不过总体上来说是可以通过2A测试的。  
```go
func (rf *Raft)leader(){
	rf.mu.Lock()
	curLogLen := len(rf.logEntries) - 1
	//记录成为leader时的term，防止被后面的操作修改
	curTerm := rf.currentTerm
	rf.mu.Unlock()

	for followerId, _ := range rf.peers{
		if followerId == rf.me{
			continue
		}
		rf.nextIndex[followerId] = curLogLen + 1
		rf.matchIndex[followerId] = 0
	}

	for{
		commitFlag := true //超半数commit只通知一次管道
		commitNum := 1
		commitL := sync.Mutex{}
		//广播消息
		for followerId, _ := range rf.peers{
			if followerId == rf.me{
				continue
			}
			//每一个节点的请求参数都不一样
			rf.mu.Lock()
			newEntries := make([]Entries, 0)
			//leader发送的term应该是创建goroutine时的term
			//否则考虑LeaderA term1给followerB term1发送第一次消息，然而延迟，B没收到消息，超时
			//B变成candidate，term=2发送选票
			//此时A已经停了一个心跳的时间，已经开启了给B发的第二次goroutine，但是还未执行
			//A投票给B,并且term=2，此时A变成follower
			//然而由于发送消息是并发goroutine，A变为follower不会停止这个goroutine的执行。
			// 如果用rf.currentTerm,此时A的term为2，执行第二个发送给B消息的goroutine。
			//candidate B收到了来自term相同的leader的消息，变为follower。
			//解决就是A和B同时变成了follower。
			appendArgs := &AppendEntriesArgs{curTerm, rf.me, rf.nextIndex[followerId]-1, rf.logEntries[rf.nextIndex[followerId]-1].Term, newEntries, rf.commitIndex}
			rf.mu.Unlock()

			go func(server int) {
				reply := &AppendEntriesReply{}
				DPrintf(rf.log, "info", "me:%2d term:%3d | leader send message to %3d\n", rf.me, curTerm, server)
				if ok := rf.peers[server].Call("Raft.AppendEntries", appendArgs, reply); ok{

					rf.mu.Lock()
					if rf.currentTerm != curTerm{
						//如果server当前的term不等于发送信息时的term
						//表明这是一条过期的信息，不要了
						rf.mu.Unlock()
						return
					}
					rf.mu.Unlock()

					if reply.Success{
						commitL.Lock()
						commitNum = commitNum + 1
						if commitFlag && commitNum > len(rf.peers)/2{
							commitFlag = true
							commitL.Unlock()
							//执行commit成功的操作
							}else{
								commitL.Unlock()
						}
					}else{
						//append失败
						rf.mu.Lock()
						if reply.Term > curTerm{
							//返回的term比发送信息时leader的term还要大
							rf.currentTerm = reply.Term
							rf.mu.Unlock()
							rf.changeRole(Follower)
						}else{
							//prevLogIndex or prevLogTerm不匹配
							rf.nextIndex[server] = rf.nextIndex[server] - 1
							rf.mu.Unlock()
						}
					}
				}
			}(followerId)
		}

		select {
		case <- rf.appendCh:
			rf.changeRole(Follower)
			return
		case <- rf.voteCh:
			rf.changeRole(Follower)
			return
		case <- time.After(time.Duration(rf.heartBeat) * time.Millisecond):
			//do nothing
		}
	}
}

```

##	candidate
candidate的逻辑如下：
*	follower超时成为candidate。
*	candidate获得超半数机器确认，升为leader。
*	投票给更高term的candidate。
*	接收leader的心跳信息，并且转为follower。
*	定时器超时，重新投票

```go

func (rf *Raft)candidate(){
	rf.mu.Lock()
	//candidate term已经在changeRole里+1了
	//发给每个节点的请求参数都是一样的。
	rf.votedFor = rf.me
	logLen := len(rf.logEntries) - 1
	requestArgs := &RequestVoteArgs{rf.currentTerm, rf.me, logLen, rf.logEntries[logLen].Term}
	rf.mu.Unlock()

	voteCnt := 1 //获得的选票，自己肯定是投给自己啦
	voteFlag := true //收到过半选票时管道只通知一次
	voteOK := make(chan bool) //收到过半选票
	voteL := sync.Mutex{}

	rf.resetTimeout()
	for followerId, _ := range rf.peers{
		if followerId == rf.me{
			continue
		}
		go func(server int) {
			reply := &RequestVoteReply{}
			if ok := rf.sendRequestVote(server, requestArgs, reply); ok{
				//ok仅仅代表得到回复，
				//ok==false代表本次发送的消息丢失，或者是回复的信息丢失
				if reply.VoteGranted{
					//收到投票
					voteL.Lock()
					voteCnt = voteCnt + 1
					if voteFlag && voteCnt > len(rf.peers)/2{
						voteFlag = false
						voteL.Unlock()
						voteOK <- true
					}else{
						voteL.Unlock()
					}
				}
			}
		}(followerId)
	}

	select {
	case args := <- rf.appendCh:
		//收到心跳信息
		DPrintf(rf.log, "info", "me:%2d term:%3d | receive heartbeat from leader %3d\n", rf.me, rf.currentTerm, args.LeaderId)
		rf.changeRole(Follower)
		return
	case args := <-rf.voteCh:
		//投票给某人
		DPrintf(rf.log, "warn", "me:%2d term:%3d | role:%12s vote to candidate %3d\n", rf.me, rf.currentTerm, rf.role, args.CandidateId)
		rf.changeRole(Follower)
		return
	case <- voteOK:
		rf.changeRole(Leader)
		return
	case <- rf.electionTimeout.C:
		//超时
		DPrintf(rf.log, "warn", "me:%2d term:%3d | candidate timeout!\n", rf.me, rf.currentTerm)
		rf.changeRole(Follower)
		return
	}
}

```

##	follower
follower负责响应leader和candidate，以及等待定时器超时。
*	收到leader的心跳信息，重启定时器。
*	投票给candidate，重启定时器。
*	定时器超时，转为candidate。

```go
func (rf *Raft)follower(){
	for{
		rf.resetTimeout()
		select {
		case args := <- rf.appendCh:
			//收到心跳信息
			DPrintf(rf.log, "info", "me:%2d term:%3d | receive heartbeat from leader %3d\n", rf.me, rf.currentTerm, args.LeaderId)
		case args := <-rf.voteCh:
			//投票给某人
			DPrintf(rf.log, "warn", "me:%2d term:%3d | role:%12s vote to candidate %3d\n", rf.me, rf.currentTerm, rf.role, args.CandidateId)
		case <- rf.electionTimeout.C:
			//超时
			DPrintf(rf.log, "warn", "me:%2d term:%3d | follower timeout!\n", rf.me, rf.currentTerm)
			rf.changeRole(Candidate)
			return
		}
	}
}
```

##	整体流程
&emsp;&emsp;本实验是使用goroutien来模拟多机环境，所以每个实例创建后，需要放在一个goroutine里运行，假装新建了一个节点，防止阻塞主程序。  
&emsp;&emsp;我的思路是实现一个无限循环的主程序run()，用于在切换角色后通过switch调动相应的函数，执行符合角色的功能。然后另写一了一个切换角色的函数changeRole,方便我打log。

初始化参数
```go
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.currentTerm = -1
	rf.votedFor = -1
	rf.role = Follower

	//log下标从1开始，0是占位符，没有意义
	rf.logEntries = make([]Entries, 1)
	//term为-1,表示第一个leader的term编号是0,test只接受int型的command
	rf.logEntries[0] = Entries{-1, -1}
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	rf.appendCh = make(chan *AppendEntriesArgs)
	rf.voteCh = make(chan *RequestVoteArgs)

	rf.log = true

	//要加这个，每次的rand才会不一样
	rand.Seed(time.Now().UnixNano())

	//心跳间隔：rf.heartBeat * time.Millisecond
	rf.heartBeat = 100

	DPrintf(rf.log, "info", "Create a new server:[%3d]! term:[%3d]\n", rf.me,rf.currentTerm)

	//主程序负责创建raft实例、收发消息、模拟网络环境
	//每个势力在不同的goroutine里运行，模拟多台机器
	go rf.run()


	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	return rf
}
```

主程序
```go
func (rf *Raft) run(){
	for{
		switch rf.role{
		case Leader:
			rf.leader()
		case Candidate:
			rf.candidate()
		case Follower:
			rf.follower()
		}
	}
}

```

修改角色
```go
func (rf *Raft)changeRole(role string){
	switch role {
	case Leader:
		DPrintf(rf.log, "warn", "me:%2d term:%3d | %12s change role to Leader!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Leader
	case Candidate:
		rf.currentTerm = rf.currentTerm + 1
		DPrintf(rf.log, "warn", "me:%2d term:%3d | %12s change role to candidate!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Candidate
	case Follower:
		DPrintf(rf.log, "warn", "me:%2d term:%3d | %12s change role to follower!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Follower
	}
}
```

##	辅助函数--log
&emsp;&emsp;在raft文件夹下，有一个util.go文件，里面提供了一个打log的函数，非常有用，方便查看。我稍稍修改了一下，将log分为info、warn、error三等，分别保存在不同的文件夹里，方便查看。与此同时，参考[网上](https://izualzhy.cn/6.824-lab2-notes)的实现，在每个log前面加上**rf.me**，通过筛选，就能快速找到属于每个节点的log。并且我还加了一个变量**rf.log**，用来控制是否输出本节点的log，当节点被调用kill()删除时，只是系统假装删除了，本质上还在，还会输出log，所以我在kill()里将**rf.log**设为*false*，通过关闭log来假装这个节点被删掉了。  

```go
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
	Info = log.New(io.MultiWriter(os.Stderr, infoFile), "Info:", log.Ldate | log.Ltime | log.Lshortfile)
	Warn = log.New(io.MultiWriter(os.Stderr, warnFile), "Warn:", log.Ldate | log.Ltime | log.Lshortfile)
	Error = log.New(io.MultiWriter(os.Stderr, errFile), "Error:", log.Ldate | log.Ltime | log.Lshortfile)
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
		Info.Printf(format, a...)
	}else if level == "warn"{
		Warn.Printf(format, a...)
	}else{
		Error.Fatalln("log error!")
	}
	return
}
```

##	requestVote
1.	请求vote的term < currentTem => false。
2.	收到更高term的投票：判断args的日志是否比当前节点新，若是则更改自己的votefor，提升自己的term，=>true，若不是，则不提升自己的term=>false。
3.	1.2.保证只有投票后才会提升自己的term，换言之，在一次选举中，一个节点只会投票给一个candidate，当一个节点的term等于请求投票的candidate的term时，代表本节点在这一轮选举中已经投过票了。

```go
//这里是站在接收者follower的角度写的
//实现接收者接到一个请求投票时的逻辑
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.currentTerm > args.Term{
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
	}

	if rf.currentTerm < args.Term{
		curLogLen := len(rf.logEntries)-1
		if args.LastLogIndex >= curLogLen && args.LastLogTerm >= rf.logEntries[curLogLen].Term{
			rf.votedFor = args.CandidateId
			rf.currentTerm = args.Term
			reply.Term = rf.currentTerm
			reply.VoteGranted = true

			go func() {rf.voteCh <- args}()
		}
	}

}
```

##	AppendEntries
*	args的term < 收到新的follower的term=>false。
*	来到这里，代表args的term大于等于当前follower的term。如果args的term>follower的term，那么修改follower的term，并且同步日志；如果args的term==follower的term，那么也是需要同步日志。如果接收者是旧leader呢？也是需要同步日志，所以旧leader的判断放在最后。
*	同步日志：如果follower的日志小于args的日志，或者follower在arg.prevlogindex处的日志和arg不符，返回false，并且更新两个参数，以方便快速匹配吻合的日志。
如果是旧leader，即便是同步日志失败，也要将旧leader转化为follower，以免存在两个follower。
*	来到这里，代表follower的日志长度大于等于args.prevlogindex，并且在prevlogindex及之前的日志都是匹配的。那么，删掉follower过长的日志，添加entries的日志。
*	判断收到AppendEntries的是否是过期的leader，若是，则转为follower。

```go
//leader调用follower的AppendEntries RPC服务
//站在follower角度完成下面这个RPC调用
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	//follower收到leader的信息
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = false
		return
	}
	//args.Term >= rf.currentTerm
	//logEntries从下标1开始，log.Entries[0]是占位符
	//所以真实日志长度需要-1
	curLogLength := len(rf.logEntries) - 1
	log_less := curLogLength < args.PrevLogIndex
	//接收者日志大于等于leader发来的日志  且  之前已经接收过日志  且  日志项不匹配
	log_dismatch := !log_less && args.PrevLogIndex > 0 && rf.logEntries[args.PrevLogIndex].Term != args.PrevLogTerm
	if log_less || log_dismatch {
		reply.Term = rf.currentTerm
		reply.Success = false
		DPrintf(rf.log, "info", "me:%2d term:%3d | receive leader:[%3d] message but not match!\n", rf.me, rf.currentTerm, args.LeaderId)
	} else {
		//接收者的日志大于等于prevlogindex
		rf.currentTerm = args.Term
		reply.Success = true
		//修改日志长度
		//找到接收者和leader（如果有）第一个不相同的日志
		leng := min(curLogLength-args.PrevLogIndex, len(args.Entries))
		i := 0
		for ; i < leng; i++ {
			if rf.logEntries[args.PrevLogIndex+i+1].Term != args.Entries[i].Term {
				rf.logEntries = rf.logEntries[:args.PrevLogIndex+i+1]
				break
			}
		}
		if i != len(args.Entries) {
			rf.logEntries = append(rf.logEntries, args.Entries[i:]...)
		}

		//修改commitIndex
		if args.LeaderCommit > rf.commitIndex {
			rf.commitIndex = min(args.LeaderCommit, len(rf.logEntries)-1)
		}
		DPrintf(rf.log, "info", "me:%2d term:%3d | receive new entries from leader:%3d, size:%3d\n", rf.me, rf.currentTerm, args.LeaderId, len(args.Entries))
	}
	//即便日志不匹配，但是也算是接收到了来自leader的日志。
	go func() { rf.appendCh <- args }()
}

```

##	获取随机时间
```go
//获取随机时间，用于选举
func (rf *Raft) randTime()int{
	basicTime := 400
	randNum :=  150
	//随机时间：basicTime + rand.Intn(randNum)
	timeout := basicTime + rand.Intn(randNum)
	return timeout
}

//重置定时器
func (rf *Raft) resetTimeout(){
	if rf.electionTimeout == nil{
		rf.electionTimeout = time.NewTimer(time.Duration(rf.randTime()) * time.Millisecond)
	}else{
		rf.electionTimeout.Reset(time.Duration(rf.randTime()) * time.Millisecond)

	}
}
```

##	实验总结
&emsp;&emsp;完成了2A，了解了raft leader election的流程，之前读论文时感觉懂了，实际实现的时候才发现需要衡量许多小细节，许多边界条件不好把握。强烈建议重构一遍代码，第一次花了一天多时间，亡羊补牢式写代码，哪里出错补哪里，虽然最后也能通过测试，但是体验非常模糊，细节丢三落四，很难构建一个完整的认识；等到重构时，因为知道大概有哪里会有哪些坑，所以一开始就会有意识的避开一些弯路，写起来顺风顺水，这时候才能体会到raft leader election整个运行调度的逻辑。还不错，继续完成后面的实验吧。  

##	坑
###	defer的坑
&emsp;&emsp;defer是return之后才会调用。一开始我在函数A里调用了函数B，并且函数B是一去不返不会再回来。当时发现leader一直在发送信息，没有报错，没有角色切换，很纳闷是为什么。后来检查代码才发现，原来是函数A申请了锁，然后defer锁的释放，但是中途去到函数B，而且一去不返，使得函数A一直没有return，也因此不会释放锁。又因为函数B之后不需要其他的锁，所以函数B就一直执行，而其他函数由于一直没有获得锁，所以处于停滞状态，无法更新任期，切换角色。有时候死锁还能看得到，虽然你不一定找得到哪里引发了死锁；但是这种一直跑，也没有停滞，日志又多的情况，也是容易让人摸不着头脑。  

###	角色切换
&emsp;&emsp;一个很关键的因素是，你必须弄清楚一个raft的实例的角色会因为什么而改变。穷举出每个角色改变的时机和诱因，然后思考各种可能的并发情况，看看会不会有超乎你意料之外的角色更改时刻。重点关注的是，并发会对角色切换造成什么影响。  
我遇到过一种情况：
1.	leaderA term0，给所有节点发送了一次心跳。
2.	由于网络延迟，followerB term0并没有收到这次心跳信号，超时变为candidateB，并给leaderA发出选举申请。
3.	在收到candidateB term1的选举之前，leaderA心跳信号超时，并发发送了第二次的的心跳信号。
4.	leaderA收到了B的投票，投票给B，并且将自己的term+1变成term1，且将自己变为follower。
5.	此时leaderA第二次给B发送心跳信号的goroutine执行（之前还未执行），此时leaderA的term也是1。
6.	candidateB此时收到term为1的leader发过来的心跳信号，认为当前选举已经决出了新leader，于是将自己变为follower。
7.	结果，此时A和B都变成了follower，集群失去了leader。  
我的解决思路是：保存leader在执行leader程序时的term，后续的rpc调用都是用这个term，而不是实时的检索leader的term，这样可以快速检查到leader转为follower的情况，还可以排除一些过期leader发出的信号，具体见上面的leader函数。  

##	实验结果
```go
//集群初始化，选择新leader
Test (2A): initial election ...
  ... Passed --   3.1  3   56    0
//leader宕机/掉线，剩下的大部分节点中选出新leader
Test (2A): election after network failure ...
  ... Passed --   4.5  3  112    0
PASS
ok      raft    7.580s

```

