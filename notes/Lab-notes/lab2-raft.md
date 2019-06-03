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

##	实验代码
###	参数定义
```go
type Entries struct{
	Term 	int  //该日志所属的Term
	Command 	interface{}
}
//
// A Go object implementing a single Raft peer.
//
//每一个raft peer都叫server，然后分为leader,candidate,follower三种角色，但是内部的状态都是一样的
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	role 	string //Leader or candidate or follower
	//int32固定格式进行persist
	currentTerm int  //该server属于哪个term
	votedFor	int //代表所投的server的下标,初始化为-1,表示本follower的选票还在，没有投票给其他人
	votedThisTerm int //判断rf.votedFor是在哪一轮承认的leader，防止一个节点同一轮投票给多个candidate

	logEntries	[]Entries //保存执行的命令，下标从1开始

	commitIndex	int //最后一个提交的日志，下标从0开始
	lastApplied	int //最后一个应用到状态机的日志，下标从0开始

	//leader独有，每一次election后重新初始化
	nextIndex	[]int //保存发给每个follower的下一条日志下标。初始为leader最后一条日志下标+1
	matchIndex	[]int //对于每个follower，已知的最后一条与该follower同步的日志，初始化为0。也相当于follower最后一条commit的日志

	appendCh	chan *AppendEntriesArgs //用于follower判断在election timeout时间内有没有收到心跳信号
	voteCh		chan *RequestVoteArgs //投票后重启定时器

	applyCh 	chan ApplyMsg //每commit一个log，就执行这个日志的命令，在实验中，执行命令=给applyCh发送信息



	log 	bool //是否输出这一个raft实例的log,若为false则不输出，相当于这个实例“死了”

	electionTimeout *time.Timer //选举时间定时器
	heartBeat	int //心跳时间定时器

}
```

###	Leader
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

###	candidate
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

###	follower
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

###	整体流程
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

###	辅助函数--log
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

###	requestVote
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

###	AppendEntries
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

###	获取随机时间
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

#	lab2B-日志复制 
##	实验要求
*	第一个测试，TestBasicAgree2B, 就是正常的日志同步。
*	第二个测试，TestFailAgree2B,在同步了一个日志后，一个节点丢失，在大多数节点同步了五六个日志后才回到集群。这时候：1.重新选举，2.快速同步日志。
*	第三个测试，TestFailNoAgree2B,共5个节点，在同步了一个日志后，有三个节点失联，测试，然后三个节点恢复，再测试。
*	第四个测试，TestConcurrentStarts2B,之前是按顺序往leader提交命令，这里是模拟多个客户端并发向leader提交命令，观察同步情况。
*	第五个测试，TestRejoint2B，目的是为了验证，当一个leader失效后（网络断开），又接受了多个命令，如何在重新加入后完成日志同步的问题。
*	第六个测试，TestBackup2B,大概就是，5台机器，决出leadaer1后，leader1和一个节点分区，然后发送多条不能commit的指令给leader1。剩下的节点中决出新leader2，发送多条可以commit的指令，然后再关闭其中一台机器，现在leader2仅和一个follower联通，此时给leader2再发送多条不能commit的指令。然后让leader1和其他两个失联的节点恢复，继续疯狂发送多条可以commit的指令。最后要求所有机器的apply顺序相同。
*	第七个测试，TestCount2B,完成日志复制过程中，所需要用到的RPC个数，不能太多，比如这里是不能超过60个rpc调用。

##	实验理解
&emsp;&emsp;lab2B要求实现日志同步，包括但不限于在各种leader失联、节点宕机、网络延迟等各种情况下保持日志的一致性，别看好像才三种异常情况，但是这三种异常各种排列组合后，将会出现许多出乎你意料之外的边界条件，需要喝杯茶，慢慢去看你自己的log才能发现错误所在。这门实验给我带来的最大的收获，除了对raft的深入理解之外，就是学会打log，别看打log好像很简单的一件事情，但是如何使log快速有效的定位和反应问题，也是一门学问。  
&emsp;&emsp;当实现lab2B时，才真正明白论文里的lastApplied和matchIndex的含义，在这里先回顾一下：
*	lastApplied：每个server最后一个执行的指令。（在本实验中执行指令=将指令发送到applyCh管道）
*	matchIndex：由leader维护，记录每个follower最后一条同步的指令。  
&emsp;&emsp;咋一看matchIndex和nextIndex好像功能有点重合？其实不是一回事，在raft中，客户端发来的指令按顺序保存在leader的log中，客户每发来一条指令，leader就将这条指令放在自己的log里。所以新来的日志的“预计”交付下标，就是这条日志到来时leader的log的长度（log下标从1开始，在下标为0处有一个占位符）。  
&emsp;&emsp;而lastApplied是每个server已经交付了的最新的日志。在leaderCommit比自己的commit大时，可以根据lastApplied，将自己LogEntries上未交付的日志有序交付，保证和其他节点一样的交付顺序。（本实验中，给applyCh交付命令=执行命令）  
&emsp;&emsp;commitIndex记录的是每个server已经commit的日志，对于follower来说，commit不一定成功，也就不一定会将日志应用到状态机上（在这里，应用到状态机就是给applyCh管道发送信息）。commitIndex记录本节点收到的消息下标，在选举时有用。对于leader来说，只有当超过半数节点响应时，才会增加自己的commitIndex，然后下一次发送心跳信号时，follower通过leaderCommit就知道哪些日志已经超半数节点确认，自己就可以交付响应的日志。  
&emsp;&emsp;matchIndex则是leader用来保存已经复制到每个follower的日志下标。比如有ABC三个日志发给123三个节点，1节点的日志都丢失了，2的丢失了1个，3的全部到达，当有新的日志D到来时，发送给每个节点的消息就是matchIndex[server]+新消息。  
&emsp;&emsp;而matchIndex和nextIndex的区别就是，假如leader先发送12两条命令，12的命令还没收到回复，此时matchIndex=0,nextIndex=3，,然后发送345三条命令，此时如果使用matchIndex来计算发送的日志，将会发送12345五条指令；而使用nextIndex来计算发送的日志，将会只发送345三条指令，大大减少了带宽！如果345的指令比12的指令先到怎么办？此时follower的log小于prevLogIndex,所以会拒绝这条命令。  

##	实验代码
比起lab2A，这次的代码根据遇到的问题微调了一下。

###	AppendEntries
参数定义，lab3C要求加上论文上讲到的优化技巧，即当follower最后一个日志的term1和leader的prevLogTerm不匹配时，返回follower第一个term1的日志的下标，这样一个冲突的日志，一次RPC就能返回，速度快很多。  
```go
// field names must start with capital letters!
type AppendEntriesReply struct {
	Term    int  //接收到信息的follower的currentTerm，方便过期leader更新信息。
	Success bool // true if follower contained entry matching prevLogIndex and PrevLogTerm

	//follower节点第一个与args.Term不相同的日志下标。
	//一个冲突的term一次append RPC就能排除
	ConflictIndex int
}

```

```go
//leader调用follower的AppendEntries RPC服务
//站在follower角度完成下面这个RPC调用
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	//follower收到leader的信息
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.ConflictIndex = -1
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = false
		return
	}

	//遇到心跳信号len(args.entries)==0不能直接返回
	//因为这时可能args.CommitIndex > rf.commintIndex
	//需要交付新的日志


	//args.Term >= rf.currentTerm
	//logEntries从下标1开始，log.Entries[0]是占位符
	//所以真实日志长度需要-1
	curLogLength := len(rf.logEntries) - 1
	log_less := curLogLength < args.PrevLogIndex
	//接收者日志大于等于leader发来的日志  且 日志项不匹配
	log_dismatch := !log_less  && rf.logEntries[args.PrevLogIndex].Term != args.PrevLogTerm
	if log_less || log_dismatch {
		reply.Term = rf.currentTerm
		reply.Success = false

		if log_dismatch{
			//日志项不匹配，将follower这一个term所有的日志回滚
			for index := curLogLength - 1; index >=0; index--{
				if rf.logEntries[index].Term != rf.logEntries[index+1].Term{
					reply.ConflictIndex = index + 1
				}
			}
		}
		if log_less{
			//如果follower日志较少
			reply.ConflictIndex = curLogLength + 1
		}
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

		if len(args.Entries) != 0{
			//心跳信号不输出
			//心跳信号可能会促使follower执行命令
			DPrintf(rf.log, "info", "me:%2d term:%3d | receive new command:%3d from leader:%3d, size:%3d\n", rf.me, rf.currentTerm, rf.logEntries[len(rf.logEntries)-1].Command, args.LeaderId, args.PrevLogIndex + len(args.Entries) - rf.commitIndex)

		}


		//修改commitIndex
		if args.LeaderCommit > rf.commitIndex {
			newCommitIndex := min(args.LeaderCommit, len(rf.logEntries)-1)
			//不能用goroutine，因为程序要求log按顺序交付
			for i := rf.lastApplied + 1; i <= newCommitIndex; i++{
				rf.applyCh <- ApplyMsg{true, rf.logEntries[i].Command, i}
			}
			rf.commitIndex = newCommitIndex
			rf.lastApplied = newCommitIndex
			DPrintf(rf.log, "info", "me:%2d term:%3d | Follower commit! cmd:%3d CommitIndex:%3d\n",rf.me ,rf.currentTerm, rf.logEntries[rf.commitIndex].Command, rf.commitIndex)
			rf.persist()
			}
	}
	//通知follower，接收到来自leader的消息
	//即便日志不匹配，但是也算是接收到了来自leader的心跳信息。
	go func() { rf.appendCh <- args }()
}

```

###	requestVote
主要小心两种情况，第一是follower一轮可能投给多个节点，第二是follower可能一轮选举中不会投给任何一个节点。详见注释。  
```go
//这里是站在接收者follower的角度写的
//实现接收者接到一个请求投票时的逻辑
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	//当candidate的term比currentTerm小时，不能直接返回
	//考虑一种情况
	//有ABC三台server，A变成了leader
	//然后A分区，注意，此时A还是leader
	//然后BC决出新leaderB，term1
	//B接收并执行了五条命令，此时C最后一条日志的term=1
	//B网络分区，此时A恢复，且收到B的信息，降为follower，term1
	//A超时，变成candidate,term2，发出投票
	//B因为网络分区不参与后续事情
	//C收到投票，因为C日志比A新，所以不投给A
	//C超时，term2,发出投票
	//A因为和C处于同一个term，不投给C
	//A超时，term3,给C发投票
	//重复
	//----------------
	//C因为A的日志比C旧，所以不投给A
	//又因为每次C发起投票时，A的term都和C一样
	//所以A不投给C
	//出现死循环，AC之间不仅没有B数，还永远都不会决出新leader
	//---------------
	//重新思考论文里投票的规则
	//其实投票规则就一个，如果candidate的日志比follower的日志更新，就将票投给candidate
	//同时为了防止同一轮选举中，一个follower投票给两个candidate
	//所以，只要follower收到投票，就增加commitIndex


	if rf.currentTerm > args.Term{
		//过期candidate
		return
	}
	if rf.currentTerm == args.Term && rf.role == Leader{
		//同一个term的leader忽略同一个term的candidate发起的投票
		return
	}
	//只要接到candidate的投票
	//就会改变自己的currentTerm
	rf.currentTerm = args.Term

	curLogLen := len(rf.logEntries)-1

	DPrintf(rf.log, "info", "me:%2d term:%3d curLogLen:%3d logTerm:%3d | candidate:%3d lastLogIndex:%3d lastLogTerm:%3d\n",
		rf.me, rf.currentTerm, curLogLen, rf.logEntries[curLogLen].Term, args.CandidateId, args.LastLogIndex, args.LastLogTerm)
	if args.LastLogTerm > rf.logEntries[curLogLen].Term || (args.LastLogTerm == rf.logEntries[curLogLen].Term && args.LastLogIndex >= curLogLen) {
		//candidate日志比本节点的日志“新”

		//判断这一轮选举内是否已经投票给某人
		if rf.votedThisTerm < args.Term{
			rf.votedFor = args.CandidateId
			rf.votedThisTerm = args.Term
			reply.Term = rf.currentTerm
			reply.VoteGranted = true

			rf.persist()

			go func() { rf.voteCh <- args }()
		}
	}

}

```

###	start
```go
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	term = rf.currentTerm
	isLeader = rf.role == Leader
	if isLeader{
		//logEntries有一个占位符，所以其长度为3时，表明里面有2个命令，而新来的命令的提交index就是3,代表是第三个提交的。
		index = len(rf.logEntries)
		rf.logEntries = append(rf.logEntries, Entries{rf.currentTerm, command})
		DPrintf(rf.log, "info", "me:%2d term:%3d | Leader receive a new command:%3d\n", rf.me, rf.currentTerm, command.(int))

		rf.persist()
		}

	return index, term, isLeader
}
```

###	changeRole
```go
//修改角色
func (rf *Raft)changeRole(role string){
	switch role {
	case Leader:
		DPrintf(rf.log, "warn", "me:%2d term:%3d | %12s change role to Leader!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Leader
	case Candidate:
		rf.currentTerm = rf.currentTerm + 1
		rf.votedThisTerm = rf.currentTerm
		rf.votedFor = rf.me
		DPrintf(rf.log, "warn", "me:%2d term:%3d | %12s change role to candidate!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Candidate
	case Follower:
		DPrintf(rf.log, "warn", "me:%2d term:%3d | %12s change role to follower!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Follower
	}
}
```

###	leader
别看leader代码好像很多（其实是注释多），其实就是论文上的思路。
```go
func (rf *Raft)leader(){

	rf.mu.Lock()
	curLogLen := len(rf.logEntries) - 1
	//记录成为leader时的term，防止被后面的操作修改
	//leader发送的term应该是创建goroutine时的term
	//否则考虑LeaderA term1给followerB term1发送第一次消息，然而延迟，B没收到消息，超时
	//B变成candidate，term=2发送选票
	//此时A已经停了一个心跳的时间，已经开启了给B发的第二次goroutine，但是还未执行
	//A投票给B,并且term=2，此时A变成follower
	//然而由于发送消息是并发goroutine，A变为follower不会停止这个goroutine的执行。
	// 如果用rf.currentTerm,此时A的term为2，执行第二个发送给B消息的goroutine。
	//candidate B收到了来自term相同的leader的消息，变为follower。
	//最后就是A和B同时变成了follower。
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
		commitFlag := true //超半数commit只修改一次leader的logEntries
		commitNum := 1  //记录commit某个日志的节点数量
		commitL := new(sync.Mutex)
		//用来通知前一半commit的节点，这个日志commit了，可以修改对应的leader.nextIndex[server]
		commitCond := sync.NewCond(commitL)

		//广播消息
		for followerId, _ := range rf.peers{
			if followerId == rf.me{
				continue
			}
			//每一个节点的请求参数都不一样
			rf.mu.Lock()
			//每个节点由于nextIndex不同，每次需要更新的日志也不同
			//不使用matchIndex而使用nextIndex的原因是
			//当发送了12没收到回复,然后发送345时
			//使用matchIndex需要发送1~5
			//而使用nextIndex只需要发送345即可，可以节省带宽
			//不用怕如果follower先收到345，因为345的prevLogIndex和follower的不匹配.
			//appendArgs := &AppendEntriesArgs{curTerm,
			//	rf.me,
			//	rf.matchIndex[followerId],
			//	rf.logEntries[rf.matchIndex[followerId]].Term,
			//	rf.logEntries[rf.matchIndex[followerId]+1:],
			//	rf.commitIndex}
			appendArgs := &AppendEntriesArgs{curTerm,
				rf.me,
				rf.nextIndex[followerId]-1,
				rf.logEntries[rf.nextIndex[followerId]-1].Term,
				rf.logEntries[rf.nextIndex[followerId]:],
				rf.commitIndex}
			rf.mu.Unlock()

			//发送心跳信息
			go func(server int) {
				reply := &AppendEntriesReply{}
				//DPrintf(rf.log, "info", "me:%2d term:%3d | leader send message to %3d\n", rf.me, curTerm, server)
				if ok := rf.peers[server].Call("Raft.AppendEntries", appendArgs, reply); ok{
					//本轮新增的日志数量
					appendEntriesLen := len(appendArgs.Entries)

					rf.mu.Lock()
					defer rf.mu.Unlock()

					if rf.currentTerm != curTerm || appendEntriesLen == 0{
						//如果server当前的term不等于发送信息时的term
						//表明这是一条过期的信息，不要了

						//或者是心跳信号，也直接返回
						return
					}

					if reply.Success{
						//append成功

						//考虑一种情况
						//第一个日志长度为A，发出后，网络延迟，很久没有超半数commit
						//因此第二个日志长度为A+B，发出后，超半数commit，修改leader
						//这时第一次修改的commit来了，因为第二个日志已经把第一次的日志也commit了
						//所以需要忽略晚到的第一次commit
						curCommitLen := appendArgs.PrevLogIndex + appendEntriesLen
						if curCommitLen >= rf.nextIndex[server]{
							rf.nextIndex[server] = curCommitLen + 1
						}else{
							return
						}


						commitCond.L.Lock()
						defer commitCond.L.Unlock()

						commitNum = commitNum + 1
						if commitFlag && commitNum > len(rf.peers)/2{
							//第一次超半数commit
							commitFlag = false


							//leader提交日志，并且修改commitIndex

							//试想，包含日志1的先commit
							//然后，包含日志1～4的后commit
							//这时候leader显然只需要交付2～4给client
							DPrintf(rf.log, "info", "me:%2d term:%3d | curCommitLen:%3d  rf.commitIndex:%3d\n",
								rf.me, rf.currentTerm, curCommitLen, rf.commitIndex)

							if curCommitLen > rf.lastApplied {
								//这一次commit的命令多于rf已经应用的命令
								//这里需要判断吗？能进来说明curCommitLen >= nextIndex[server]
								//写博客时才发现这里忘了验证，算了
								//说明这一次进来的，一定有还未被提交的命令

								//本轮commit的日志长度大于leader当前的commit长度
								//假如原来日志长度为10
								//发送了1的日志，然后又发送了1~4的日志
								//先commit了1的日志，长度变11
								//然后接到1~4的commit，curCommitLen=14
								//curCommitLen和leader当前日志的差是3
								//所以leader只需要commit本次entries的后3个命令即可。

								//leader给client commit这次日志
								for i := rf.lastApplied + 1; i <= curCommitLen; i++ {
									//leader将本条命令应用到状态机
									rf.applyCh <- ApplyMsg{true, rf.logEntries[i].Command, i}
									//通知client，本条命令成功commit
									//上面必须for循环，因为消息要按顺序执行
									DPrintf(rf.log, "info", "me:%2d term:%3d | Leader Commit:%4d OK, commitIndex:%3d\n",
										rf.me, rf.currentTerm, rf.logEntries[i].Command, i)
								}
								rf.lastApplied = curCommitLen
								rf.commitIndex = curCommitLen
							}

						}
						//只要是成功commit的follower，就修改其matchIndex
						rf.matchIndex[server] = curCommitLen

					}else{
						//append失败
						if reply.Term > curTerm{
							//返回的term比发送信息时leader的term还要大
							rf.currentTerm = reply.Term
							rf.changeRole(Follower)
							//暂时不知道新leader是谁，等待新leader的心跳信息
							rf.appendCh <- &AppendEntriesArgs{rf.currentTerm, -1, -1, -1, make([]Entries, 0), -1}
						}else{
							//prevLogIndex or prevLogTerm不匹配
							rf.nextIndex[server] = reply.ConflictIndex
							DPrintf(rf.log, "info", "me:%2d term:%3d | Msg to %3d append fail,decrease nextIndex to:%3d\n",
								rf.me, rf.currentTerm, server, rf.nextIndex[server])
						}
					}
				}
			}(followerId)
		}


		select {
		case args := <- rf.appendCh:
			DPrintf(rf.log, "warn", "me:%2d term:%3d | new leader:%3d , leader convert to follower!\n",
				rf.me, rf.currentTerm, args.LeaderId)
			rf.changeRole(Follower)
			return
		case args := <- rf.voteCh:
			DPrintf(rf.log, "warn", "me:%2d term:%3d | role:%12s vote to candidate %3d\n", rf.me, rf.currentTerm, rf.role, args.CandidateId)
			rf.changeRole(Follower)
			return
		case <- time.After(time.Duration(rf.heartBeat) * time.Millisecond):
			//do nothing
		}
	}
}

```

###	candidate
```go

func (rf *Raft)candidate(){
	rf.mu.Lock()
	//candidate term已经在changeRole里+1了
	//发给每个节点的请求参数都是一样的。
	logLen := len(rf.logEntries) - 1
	requestArgs := &RequestVoteArgs{rf.currentTerm, rf.me, logLen, rf.logEntries[logLen].Term}
	rf.persist()
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
	case  <- rf.appendCh:
		//收到心跳信息
		//case args <- rf.appendCh:
		//DPrintf(rf.log, "info", "me:%2d term:%3d | receive heartbeat from leader %3d\n", rf.me, rf.currentTerm, args.LeaderId)
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

###	follower
```go
func (rf *Raft)follower(){
	for{
		rf.resetTimeout()
		select {
		case  <- rf.appendCh:
			//收到心跳信息
			//DPrintf(rf.log, "info", "me:%2d term:%3d | receive heartbeat from leader %3d\n", rf.me, rf.currentTerm, args.LeaderId)
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

##	实验结果
```go
//要求real小于一分钟，user小于5s
Test (2B): basic agreement ...
  ... Passed --   1.0  5   32    3
Test (2B): agreement despite follower disconnection ...
  ... Passed --   6.3  3  128    8
Test (2B): no agreement if too many followers disconnect ...
  ... Passed --   3.9  5  180    4
Test (2B): concurrent Start()s ...
  ... Passed --   0.7  3    8    6
Test (2B): rejoin of partitioned leader ...
  ... Passed --   6.8  3  200    4
Test (2B): leader backs up quickly over incorrect follower logs ...
  ... Passed --  25.8  5 2072  102
Test (2B): RPC counts aren't too high ...
  ... Passed --   2.4  3   42   12
PASS
ok  	raft	46.885s

real    0m47.473s
user    0m0.796s
sys     0m2.276s
```

##	坑
&emsp;&emsp;管道必须初始化！然而不初始化管道直接使用也不会报错，此时管道就和没有一样。只会停等！

#	lab2C-持久化数据
&emsp;&emsp;老实说，不是很清楚为什么拿2C单独作为lab2的一部分，理论上来说，只要在交付信息的同时持久化数据不就可以了吗。所以我也只是在lab2B的基础上，在相应apply日志的地方插上一句rf.persist()，然后一次性就通过了。

##	分析
&emsp;&emsp;因为要求各server apply的日志顺序和数量是相同的，而同时也希望server在宕机后，通过持久化数据恢复过来后，也能和其他server保持一致，所以我就只保存apply的数据。这样的好处在于数据一定可以一致。坏处在于，可能会丢失一些信息？但我是apply一次就持久化一次，应该不会丢失太多吧。当然，如果说server在apply之后，持久化之前宕机这也是无可奈何的。其实不是很清楚为什么不持久化lastApplied，就算server从宕机恢复后，没有LastApplied也不知道哪些日志已经执行了啊。噢，也许这就是快照的意义所在？一个server宕机后恢复，可以理解为这个server重置回初始状态，需要将logs里的指令重新按顺序执行一遍。而快照的意义在于，可以定时保存server的状态和已经执行的日志，那么，当server宕机后，可以直接恢复到快照对应的状态，并且执行后续未执行的日志。  
&emsp;&emsp;分析一下持久化数据term、votedFor、log[]可能会改变的情况：
*	follower： 
	1.	在投票给某人（在requestVote）
	2.	收到appnd并且要交付信息到applyCh时才persist（在appendEntries）
	3.	转为candidate时（在candidate函数里持久化）
*	andidate：
	1.	投票给其他人（修改了currentTerm和votedFor)（在requestVote）
	2.	收到append并且要交付信息（在appendEntries）
*	Leader:
	1.	从start()函数收到新信息时保存，此时会更新logEntries
	2.	投票给更高term的candidate（在requestVote）
	3.	收到更高term的leader（在appendEntries）  
综上，只要修改 start  requestVote appendEntries candidate四个函数即可。（代码见lab2B）

##	实验代码
### 持久化代码
```go
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)

	//这里是假设rf.persist()都在已经拿到锁的时候调用
	//所以这里不申请锁
	w := new(bytes.Buffer)
	enc := labgob.NewEncoder(w)
	enc.Encode(rf.currentTerm)
	enc.Encode(rf.votedFor)
	enc.Encode(rf.logEntries)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
	DPrintf(rf.log, "info", "me:%2d term:%3d | Role:%10s Persist data! VotedFor:%3d len(Logs):%3d\n", rf.me, rf.currentTerm, rf.role, rf.votedFor, len(rf.logEntries))
}
```

###	读取保存的代码
```go
unc (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }

	//只有一个raft启动时才调用此函数，所以不申请锁
	r := bytes.NewBuffer(data)
	dec := labgob.NewDecoder(r)

	var term	int
	var votedFor	int
	var logs []Entries

	//还没运行之前调用此函数
	//所以不用加锁了吧
	if dec.Decode(&term) != nil || dec.Decode(&votedFor) !=nil || dec.Decode(&logs) != nil{
		DPrintf(rf.log, "info", "me:%2d term:%3d | Failed to read persist data!\n")
	}else{
		rf.currentTerm = term
		rf.votedFor = votedFor
		rf.logEntries = logs
		DPrintf(rf.log, "info", "me:%2d term:%3d | Read persist data successful! VotedFor:%3d len(Logs):%3d\n", rf.me, rf.currentTerm, rf.votedFor, len(rf.logEntries))
	}
}
```

##	实验结果
```go
Test (2C): basic persistence ...
  ... Passed --   4.6  3  252    6
Test (2C): more persistence ...
  ... Passed --  24.9  5 2216   19
Test (2C): partitioned leader and one follower crash, leader restarts ...
  ... Passed --   2.3  3   60    4
Test (2C): Figure 8 ...
  ... Passed --  32.2  5 29736   14
Test (2C): unreliable agreement ...
  ... Passed --   5.5  5  212  246
Test (2C): Figure 8 (unreliable) ...
  ... Passed --  37.6  5 3668  576
Test (2C): churn ...
  ... Passed --  16.3  5  928  385
Test (2C): unreliable churn ...
  ... Passed --  16.4  5  848  226
PASS
ok  	raft	139.815s
```

##	综合实验结果
```go
A+B+C要求四分钟内完成，且cpu时间小于一分钟。
Test (2A): initial election ...
  ... Passed --   3.0  3   54    0
Test (2A): election after network failure ...
  ... Passed --   4.5  3  118    0
Test (2B): basic agreement ...
  ... Passed --   1.0  5   32    3
Test (2B): agreement despite follower disconnection ...
  ... Passed --   6.4  3  130    8
Test (2B): no agreement if too many followers disconnect ...
  ... Passed --   3.8  5  176    4
Test (2B): concurrent Start()s ...
  ... Passed --   0.7  3    8    6
Test (2B): rejoin of partitioned leader ...
  ... Passed --   6.6  3  192    4
Test (2B): leader backs up quickly over incorrect follower logs ...
  ... Passed --  28.3  5 2168  102
Test (2B): RPC counts aren't too high ...
  ... Passed --   2.3  3   42   12
Test (2C): basic persistence ...
  ... Passed --   4.9  3  246    6
Test (2C): more persistence ...
  ... Passed --  25.3  5 2200   19
Test (2C): partitioned leader and one follower crash, leader restarts ...
  ... Passed --   2.4  3   60    4
Test (2C): Figure 8 ...
  ... Passed --  30.5  5 27540   17
Test (2C): unreliable agreement ...
  ... Passed --   5.8  5  224  246
Test (2C): Figure 8 (unreliable) ...
  ... Passed --  37.3  5 3532  305
Test (2C): churn ...
  ... Passed --  16.3  5 1524  177
Test (2C): unreliable churn ...
  ... Passed --  16.3  5 1140  272
PASS
ok  	raft	195.444s
```

##	坑
&emsp;&emsp;刚才说一次完成，其实是不准确的。后来多跑了几次，发现lab2C的TestFigure8Unreliable2C偶尔会出现错误，当测试运行超过40s时就会出错，而我的程序5次大概有一次会超过40s报错吧。检查一下代码，应该是在某种情况下选举时间太久的原因，减少定时器时间，或者重构一下代码，减少选举需要的RPC数量也许就能彻底解决这个问题。  

#	实验总结
&emsp;&emsp;连续三四天朝九晚十，终于独立完成通过lab2。这一次实现并没有考虑data race，几乎都不通过，但是test全部都通过了。对于raft有了本质的认识，虽然只是一个小玩具，但是大概了解了一个分布式系统是怎么搭建的，运行的内核是怎样建立的，虽说有些概念还有些模糊，但也是获益匪浅。休息一下，过几天准备参考[这里](https://github.com/shishujuan/mit6.824-2017-raft)重构一下代码。  
