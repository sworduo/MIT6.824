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
	DPrintf(rf.log, "info", "Server:%3d role:%12s isleader:%t\n", rf.me, rf.role, isleader)
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
		if args.LastLogIndex >= curLogLen && args.LastLogTerm >= rf.logEntries[curLogLen].Term{
			rf.votedFor = args.CandidateId
			rf.currentTerm = args.Term
			reply.Term = rf.currentTerm
			reply.VoteGranted = true

			go func() {rf.voteCh <- args}()
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