package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"math/rand"
	"sync"
	"time"
)
import "labrpc"

// import "bytes"
// import "labgob"



//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

//term
const(
	Leader = "Leader"
	Candidate = "Candidate"
	Follower = "Follower"
)

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
	currentTerm int  //该server属于哪个term
	votedFor	int //代表所投的server的下标,初始化为-1,表示本follower的选票还在，没有投票给其他人
	logEntries	[]Entries //保存执行的命令，下标从1开始

	commitIndex	int //最后一个提交的日志，下标从0开始
	lastApplied	int //最后一个应用到状态机的日志，下标从0开始

	//leader独有，每一次election后重新初始化
	nextIndex	[]int //保存发给每个follower的下一条日志下标。初始为leader最后一条日志下标+1
	matchIndex	[]int //对于每个follower，已知的最后一条与该follower同步的日志，初始化为0。也相当于follower最后一条commit的日志

	appendCh	chan *AppendEntriesArgs //用于follower判断在election timeout时间内有没有收到心跳信号
	voteCh		chan *RequestVoteArgs //投票后重启定时器

	applyCh 	chan ApplyMsg //每commit一个log，就执行这个日志的命令，在实验中，执行命令=给applyCh发送信息
	newCmd 		chan interface{} //leader接收新命令
	commitToClient chan int //超半数commit，发送给client


	log 	bool //是否输出这一个raft实例的log,若为false则不输出，相当于这个实例“死了”

	electionTimeout *time.Timer //选举时间定时器
	heartBeat	int //心跳时间定时器

}

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

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	//加锁是因为，一个server的term可能会因为超时或者取得超半数选票而改变
	rf.mu.Lock()
	term = rf.currentTerm
	isleader = (rf.role == Leader)
	DPrintf(rf.log, "warn", "Server:%3d role:%12s isleader:%t\n", rf.me, rf.role, isleader)
	rf.mu.Unlock()
	return term, isleader
}


//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}


//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
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
}


// field names must start with capital letters!
type AppendEntriesArgs struct{
	Term 	int //Leader's term
	LeaderId 	int
	//PreLogIndex和PrevLogTerm用来确定leader和收到这条信息的follower上一条同步的信息
	//方便回滚，或者是新leader上线后覆盖follower的日志
	PrevLogIndex 	int //index of log entry immediately preceding new ones
	PrevLogTerm 	int //term of prevLogIndex entry
	Entries 	[]Entries //log entries to store(empty for heartbeat;may send more than one for efficiency)
	LeaderCommit 	int //leader's commitIndex
}

// field names must start with capital letters!
type AppendEntriesReply struct {
	Term    int  //接收到信息的follower的currentTerm，方便过期leader更新信息。
	Success bool // true if follower contained entry matching prevLogIndex and PrevLogTerm
}

func min(a, b int) int {
	if a < b{
		return a
	}
	return b
}
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
	//接收者日志大于等于leader发来的日志  且 日志项不匹配
	log_dismatch := !log_less  && rf.logEntries[args.PrevLogIndex].Term != args.PrevLogTerm
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

		if args.PrevLogIndex + len(args.Entries) > rf.commitIndex{
			DPrintf(rf.log, "info", "me:%2d term:%3d | receive new command:%3d from leader:%3d, size:%3d\n", rf.me, rf.currentTerm, rf.logEntries[len(rf.logEntries)-1].Command, args.LeaderId, args.PrevLogIndex + len(args.Entries) - rf.commitIndex)

		}
		//修改commitIndex
		if args.LeaderCommit > rf.commitIndex {
			newCommitIndex := min(args.LeaderCommit, len(rf.logEntries)-1)
			//不能用goroutine，因为程序要求log按顺序交付
			for i := rf.commitIndex + 1; i <= newCommitIndex; i++{
				rf.applyCh <- ApplyMsg{true, rf.logEntries[i].Command, i}
			}
			rf.commitIndex = newCommitIndex
			DPrintf(rf.log, "info", "me:%2d term:%3d | Follower commit! cmd:%3d New commit index:%3d\n",rf.me ,rf.currentTerm, rf.logEntries[rf.commitIndex].Command, rf.commitIndex)
		}
	}
	//即便日志不匹配，但是也算是接收到了来自leader的心跳信息。
	go func() { rf.appendCh <- args }()
}


//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term 	int
	CandidateId	int	//发出选票的candidate的id，这里是server中的下标
	//LastLogIndex和LastLogTerm合在一起用来比较candidate和follower谁更“新”
	LastLogIndex	int //发出选票的candidate的最后一个日志的下标
	LastLogTerm	int	//发出选票的candidate的最后一个日志对应的term
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term 	int 	//返回接收者的currentTerm，一般是针对过期leader
	//如果candidate term < 接收者 term ==>false
	//如果接收者的votedFor == (null or candidateId)
	//并且candidate的日志和接收者的日志一样“新“ ==> true 表示我投票给你了
	//接收者投票之后会改变自己的voterFor
	VoteGranted	bool
}

//
// example RequestVote RPC handler.
//
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
		DPrintf(rf.log, "info", "me:%2d term:%3d | curLogLen:%3d candidate:%3d lastLogIndex:%3d\n",
			rf.me, rf.currentTerm, curLogLen, args.CandidateId, args.LastLogIndex)
		if args.LastLogIndex >= curLogLen && args.LastLogTerm >= rf.logEntries[curLogLen].Term{
			rf.votedFor = args.CandidateId
			rf.currentTerm = args.Term
			reply.Term = rf.currentTerm
			reply.VoteGranted = true

			go func() {rf.voteCh <- args}()
		}else{
			reply.Term = rf.currentTerm
			reply.VoteGranted = false
		}
	}

}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
//这里是站在candidate的角度来写的
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}


//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).
	rf.mu.Lock()
	term = rf.currentTerm
	isLeader = rf.role == Leader
	rf.mu.Unlock()
	if isLeader{
		go func() {
			rf.newCmd <- command
		}()
		DPrintf(rf.log, "info", "me:%2d term:%3d | Leader receive a new command:%3d\n", rf.me, rf.currentTerm, command.(int))

		select {
		case ind := <- rf.commitToClient:
			index = ind
		case <- time.After(3 * time.Second):
			//3s未返回，这个leader多半是挂了
			DPrintf(rf.log, "info", "\n")
			DPrintf(rf.log, "info", "me:%2d | More than 3 second!\n", rf.me)
			DPrintf(rf.log, "info", "\n")
			isLeader = false
		}
	}

	//=======================================================================================
	//								========               =============
	//=======================================================================================
	//======================================               ==================================
	//=======================================================================================
	//=======================================================================================
	//===================      ===============================================          =====
	//下面这里有问题，当leader接收命令时，还没有执行，commitIndex还未增加，所以直接返回是错误的
	//但是，想这样直接+1也是不行的
	//index = rf.commitIndex+1
	//	term = rf.currentTerm
	//可以增加一个管道，当leader超半数commit时，再commit这个东西。然后增加一个超时，超时不回，这个leader估计挂了。


	return index, term, isLeader
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
//关闭这一个raft实例的log
func (rf *Raft) Kill() {
	// Your code here, if desired.
	DPrintf(rf.log, "warn", "Sever index:[%3d]  Term:[%3d]  has been killed.Turn off its log\n", rf.me, rf.currentTerm)
	rf.log = false
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
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

	rf.applyCh = applyCh
	rf.commitToClient = make(chan int)

	rf.log = true

	rf.newCmd = make(chan interface{})
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


//修改角色
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

	//newEntries是每次新增的日志
	newEntries := make([]Entries, 0)

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
			prevLogInd := min(rf.nextIndex[followerId]-1, len(rf.logEntries)-1)
			addEntries := append(rf.logEntries[prevLogInd+1:], newEntries...)
			appendArgs := &AppendEntriesArgs{curTerm,
				rf.me,
				prevLogInd,
				rf.logEntries[prevLogInd].Term,
				addEntries,
				rf.commitIndex}
			rf.mu.Unlock()

			//发送心跳信息
			go func(server int) {
				reply := &AppendEntriesReply{}
				//DPrintf(rf.log, "info", "me:%2d term:%3d | leader send message to %3d\n", rf.me, curTerm, server)
				if ok := rf.peers[server].Call("Raft.AppendEntries", appendArgs, reply); ok{
					appendEntriesLen := len(appendArgs.Entries)
					rf.mu.Lock()
					if rf.currentTerm != curTerm || appendEntriesLen == 0{
						//如果server当前的term不等于发送信息时的term
						//表明这是一条过期的信息，不要了

						//或者是心跳信号，也直接返回
						rf.mu.Unlock()
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
							//前N/2如果nextIndex[server]直接+1
							//然而leader的logEntries还未增加
							//下一轮生成appendArgs时就会超过leader logEntries的索引
							rf.nextIndex[server] = curCommitLen + 1
						}else{
							rf.mu.Unlock()
							return
						}
						rf.mu.Unlock()

						commitCond.L.Lock()
						commitNum = commitNum + 1
						if commitNum > len(rf.peers)/2{
							//第一个超半数commit的节点
							if commitFlag{
								commitFlag = false
								//通知前N/2个commit的节点，这次commit有效
								commitCond.Broadcast()
								commitCond.L.Unlock()
								//修改leader的日志和commitIndex


								rf.mu.Lock()
								//试想，包含日志1的先commit
								//然后，包含日志1～4的后commit
								//这时候leader显然只需要交付2～4给client
								DPrintf(rf.log, "info", "me:%2d term:%3d | curCommitLen:%3d  rf.commitIndex:%3d\n",
									rf.me, rf.currentTerm, curCommitLen, rf.commitIndex)
								if curCommitLen > rf.commitIndex{
									//本轮commit的日志长度大于leader当前的commit长度
									//假如原来日志长度为10
									//发送了1的日志，然后又发送了1~4的日志
									//先commit了1的日志，长度变11
									//然后接到1~4的commit，curCommitLen=14
									//curCommitLen和leader当前日志的差是3
									//所以leader只需要commit本次entries的后3个命令即可。
									gap := curCommitLen - rf.commitIndex
									rf.logEntries = append(rf.logEntries, appendArgs.Entries[(appendEntriesLen - gap):]...)


									//leader给client commit这次日志
									for i := 0; i < gap; i++{
										//leader将本条命令应用到状态机
										rf.applyCh <- ApplyMsg{true, appendArgs.Entries[appendEntriesLen-gap+i].Command, rf.commitIndex+i+1}
										//通知client，本条命令成功commit
										//上面必须for循环，因为消息要按顺序执行
										//下面可以用goroutine，因为使用键值对来存储。key-commitIndex
										go func(commitIndex int) {
											rf.commitToClient <- commitIndex
											DPrintf(rf.log, "info", "me:%2d term:%3d | Leader Commit:%4d OK, commitIndex:%3d\n",
												rf.me, rf.currentTerm, rf.logEntries[rf.commitIndex].Command, commitIndex)
										}(rf.commitIndex+i+1)
									}
									rf.commitIndex = curCommitLen}
								rf.mu.Unlock()
							}else{
								commitCond.L.Unlock()
							}
						}else{
							//对于前N/2那些节点，等待这次commit成功并修改rf.nextIndex[server]
							commitCond.Wait()
							if commitNum == -1{
								return
							}
							commitCond.L.Unlock()
							rf.mu.Lock()
							rf.nextIndex[server] = rf.nextIndex[server] + len(appendArgs.Entries)
							rf.mu.Unlock()
						}
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
							rf.nextIndex[server] = rf.nextIndex[server] - 1
							DPrintf(rf.log, "info", "me:%2d term:%3d | Msg to %3d append fail,decrease nextIndex to:%3d\n",
								rf.me, rf.currentTerm, server, rf.nextIndex[server])
						}
						rf.mu.Unlock()
					}
				}
			}(followerId)
		}

		go func() {
			//对于每一次的commit，设立一个定时器
			//超时后，关闭掉所有还在等待commit的goroutine
			//10s都等不到超半数commit的信号
			//这次commit多半是失败了，直接关闭吧。
			time.Sleep(10 * time.Second)
			commitCond.L.Lock()
			commitNum = -1
			commitCond.Broadcast()
			commitCond.L.Unlock()
		}()

		//每次重设下一轮的日志
		newEntries = make([]Entries, 0)

		select {
		case cmd := <- rf.newCmd:
			newEntries = append(newEntries, Entries{curTerm, cmd})
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

	//当超半数节点commit同一个日志时，leader修改自己的log，并且将日志commit给client。
	//还需要修改对应的nextIndex

	//两个问题：前N/2成功的节点应该怎么办？这时候肯定是不更新他们的nextIndex
	//等到全部commit完才更新nextIndex?不行，这就违背了超半数节点commit就提交的原则
	//所以，超半数之前的节点commit时，停等，第一个超半数的节点到达后，广播信息，修改前半数节点的nextIndex
	//后续的节点直接修改nextIndex即可。

	//如果commit失败呢？之前停等的goroutine如何处理？
	//每个节点可以等待一段时间，如果超过某个时间没收到超半数节点commit的信号，就结束这个goroutine
	//这样一来，leader的nextIndex没有改变，不影响后续，而且不怕造成大量的处于停等状态的goroutine
	//然而cond.L.wait不能放在select里。。

	//可能的问题：后一次修改，在前一次修改前完成。比如第一次修改任务A,第二次修改任务B,然而B比A先完成
	//不影响，因为A未完成，nextIndex没变，所以B肯定包含A需要修改的内容
	//A在B后到达？所以修改nextIndex时，需要判断是否需要修改nextIndex

	//引申而来，每一次，每个节点appendEntries里的entries都是不一样的。有的可能需要加上之前没有commit的日志(这些日志可以在leader的日志里找到)
	//每个节点的日志复制：rf.logEntries[rf.nextIndex[server]:]+新的需要同步的日志


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