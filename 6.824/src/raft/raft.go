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
	"bytes"
	"labgob"
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
	CommandValid bool  //true为正常信息交付，false为快照
	Command      interface{}
	CommandIndex int
	CommitTerm 	int //交付信息时raft的term
	Role 	string //交付信息时raft的角色
}

//term
const(
	Leader = "Leader"
	Candidate = "Candidate"
	Follower = "Follower"
	votedNull = -1 //本轮没有投票给任何人
	heartBeat = time.Duration(100) //leader的心跳时间
	RPC_CALL_TIMEOUT = time.Duration(500) * time.Millisecond//rpc超时时间
)

type Entries struct{
	Term 	int  //该日志所属的Term
	Index 	int  //该日志在log的index
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

	logs	[]Entries //保存执行的命令，下标从1开始

	commitIndex	int //最后一个提交的日志，下标从0开始
	lastApplied	int //最后一个应用到状态机的日志，下标从0开始

	//leader独有，每一次election后重新初始化
	nextIndex	[]int //保存发给每个follower的下一条日志下标。初始为leader最后一条日志下标+1
	matchIndex	[]int //对于每个follower，已知的最后一条与该follower同步的日志，初始化为0。也相当于follower最后一条commit的日志

	appendCh	chan bool //用于follower判断在election timeout时间内有没有收到心跳信号
	voteCh		chan bool //投票后重启定时器
	exitCh 		chan bool //结束实例
	leaderCh 	chan bool //candidate竞选leader

	applyCh 	chan ApplyMsg //每commit一个log，就执行这个日志的命令，在实验中，执行命令=给applyCh发送信息

	lastIncludedIndex 	int  //现存快照对应的最后一个日志下标
	lastIncludedTerm 	int  //现存快照对应的最后一个日志所属term

}

//获取随机时间，用于选举
func (rf *Raft) electionTimeout() time.Duration{
	rtime := 300 + rand.Intn(150)
	//随机时间：basicTime + rand.Intn(randNum)
	timeout := time.Duration(rtime)  * time.Millisecond
	return timeout
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
	//DPrintf(rf.log, "WarnRaft", "Server:%3d role:%12s isleader:%t\n", rf.me, rf.role, isleader)
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

	//这里是假设rf.persist()都在已经拿到锁的时候调用
	//所以这里不申请锁
	w := new(bytes.Buffer)
	enc := labgob.NewEncoder(w)
	enc.Encode(rf.currentTerm)
	enc.Encode(rf.votedFor)
	enc.Encode(rf.logs)
	enc.Encode(rf.lastIncludedIndex)
	enc.Encode(rf.lastIncludedTerm)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
	InfoRaft.Printf("Raft:%2d term:%3d | Persist data! Size:%5v logs:%4v\n", rf.me, rf.currentTerm, len(data), len(rf.logs)-1)
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

	//只有一个raft启动时才调用此函数，所以不申请锁
	r := bytes.NewBuffer(data)
	dec := labgob.NewDecoder(r)

	var term	int
	var votedFor	int
	var logs []Entries
	var lastIncludedIndex int
	var lastIncludedTerm int

	//还没运行之前调用此函数
	//所以不用加锁了吧
	if dec.Decode(&term) != nil || dec.Decode(&votedFor) !=nil || dec.Decode(&logs) != nil || dec.Decode(&lastIncludedIndex) != nil || dec.Decode(&lastIncludedTerm) != nil{
		InfoRaft.Printf("Raft:%2d term:%3d | Failed to read persist data!\n")
	}else{
		rf.currentTerm = term
		rf.votedFor = votedFor
		rf.logs = logs
		rf.lastIncludedIndex = lastIncludedIndex
		rf.lastIncludedTerm = lastIncludedTerm
		rf.lastApplied = rf.lastIncludedIndex
		rf.commitIndex = rf.lastIncludedIndex
		InfoRaft.Printf("Raft:%2d term:%3d | Read persist data{%5d bytes} successful! VotedFor:%3d len(Logs):%3d\n",
			rf.me, rf.currentTerm, len(data), rf.votedFor, len(rf.logs))
	}
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

	//follower节点第一个与args.Term不相同的日志下标。
	//一个冲突的term一次append RPC就能排除
	//如果follower和leader隔了好几个term
	//只要找到leader中等于confilctTerm的最后一个日志，就能一次性添加所有follower缺失的日志
	ConflictIndex int //冲突日志处的下标
	ConflictTerm int //冲突日志处（term不匹配或者follower日志较少）的term
}

func min(a, b int) int {
	if a < b{
		return a
	}
	return b
}
func max(a, b int) int {
	if a < b{
		return b
	}
	return a
}

//leader调用follower的AppendEntries RPC服务
//站在follower角度完成下面这个RPC调用
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	//follower收到leader的信息
	rf.mu.Lock()
	//defer后进先出
	defer rf.mu.Unlock()


	reply.Term = rf.currentTerm
	reply.Success = false
	reply.ConflictIndex = -1
	reply.ConflictTerm = -1

	if args.Term < rf.currentTerm {
		return
	}

	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term
		reply.Term = rf.currentTerm
		rf.convertRoleTo(Follower)
	}
	//遇到心跳信号len(args.entries)==0不能直接返回
	//因为这时可能args.CommitIndex > rf.commintIndex
	//需要交付新的日志

	//通知follower，接收到来自leader的消息
	//即便日志不匹配，但是也算是接收到了来自leader的心跳信息。
	rf.dropAndSet(rf.appendCh)

	//args.Term >= rf.currentTerm
	//logs从下标1开始，log.Entries[0]是占位符
	//所以真实日志长度需要-1

	//如果reply.confictIndex == -1表示follower缺少日志或者leader发送的日志已经被快照
	//leader需要将nextIndex设置为conflicIndex
	if rf.getLastLogIndex() < args.PrevLogIndex{
		//如果follower日志较少
		reply.ConflictTerm = -1
		reply.ConflictIndex = rf.getLastLogIndex() + 1
		InfoRaft.Printf("Raft:%2d term:%3d | receive leader:[%3d] message but lost any message! curLen:%4d prevLoglen:%4d len(Entries):%4d\n",
			rf.me, rf.currentTerm, args.LeaderId, rf.getLastLogIndex(), args.PrevLogIndex, len(args.Entries))
		return
		}

	if rf.lastIncludedIndex > args.PrevLogIndex{
		//已经快照了
		//让leader的nextIndex--，直到leader发送快照
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		reply.ConflictTerm = -1
		return
	}

	//接收者日志大于等于leader发来的日志  且 日志项不匹配
	if args.PrevLogTerm != rf.logs[rf.subIdx(args.PrevLogIndex)].Term{
		//日志项不匹配，找到follower属于这个term的第一个日志，方便回滚。
		reply.ConflictTerm = rf.logs[rf.subIdx(args.PrevLogIndex)].Term
		for i := args.PrevLogIndex; i > rf.lastIncludedIndex ; i--{
			if rf.logs[rf.subIdx(i)].Term != reply.ConflictTerm{
				break
			}
			reply.ConflictIndex = i
		}
		InfoRaft.Printf("Raft:%2d term:%3d | receive leader:[%3d] message but not match!\n", rf.me, rf.currentTerm, args.LeaderId)
		//InfoRaft.Printf("Raft:%2d term:%3d | index:%4d | args: term%4d | cur: term:%4d", rf.me, rf.currentTerm, args.PrevLogIndex, args.PrevLogTerm, rf.logs[rf.subIdx(args.PrevLogIndex)].Term)
		return
	}

	//接收者的日志大于等于prevlogindex，且在prevlogindex处日志匹配
	rf.currentTerm = args.Term
	reply.Success = true

	//修改日志长度
	//找到接收者和leader（如果有）第一个不相同的日志
	i := 0
	for  ; i < len(args.Entries); i++{
		ind := rf.subIdx(i + args.PrevLogIndex + 1)
		if ind < len(rf.logs) && rf.logs[ind].Term != args.Entries[i].Term{
			//修改不同的日志, 截断+新增
			rf.logs = rf.logs[:ind]
			rf.logs = append(rf.logs, args.Entries[i:]...)
			break
		}else if ind >= len(rf.logs){
			//添加新日志
			rf.logs = append(rf.logs, args.Entries[i:]...)
			break
		}
	}


	if len(args.Entries) != 0{
		//心跳信号不输出
		//心跳信号可能会促使follower执行命令

		//心跳信号不改变currentTerm、votedFor、logs，改变角色的函数有persist
		rf.persist()
		InfoRaft.Printf("Raft:%2d term:%3d | receive new command from leader:%3d, term:%3d, size:%3d curLogLen:%4d LeaderCommit:%4d rfCommit:%4d\n",
			rf.me, rf.currentTerm, args.LeaderId, args.Term, len(args.Entries), len(rf.logs)-1, args.LeaderCommit, rf.commitIndex)
	}

	//修改commitIndex
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, rf.addIdx(len(rf.logs)-1))
		rf.applyLogs()
		}



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

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	if rf.currentTerm < args.Term{
		//进入新一轮投票
		//就会改变自己的currentTerm
		//并将自己转为follower
		rf.currentTerm = args.Term
		rf.convertRoleTo(Follower)
	}

	//candidate日志比本节点的日志“新”
	newerEntries := args.LastLogTerm > rf.getLastLogTerm() || (args.LastLogTerm == rf.getLastLogTerm() && args.LastLogIndex >= rf.getLastLogIndex())
	//判断这一轮term内是否已经投票给某人
	//投票给某人的信息可能会丢失，所以需要加上另一个判断
	voteOrNot := rf.votedFor == votedNull || rf.votedFor == args.CandidateId

	if  newerEntries && voteOrNot{
			rf.votedFor = args.CandidateId
			reply.VoteGranted = true

			rf.dropAndSet(rf.voteCh)
			WarnRaft.Printf("Raft:%2d term:%3d | vote to candidate %3d\n", rf.me, rf.currentTerm, args.CandidateId)
			rf.persist()
	}
}

func (rf *Raft) getLastLogIndex() int {
	//logs下标从1开始，logs[0]是占位符
	return rf.addIdx(len(rf.logs)-1)
}

func (rf *Raft) getLastLogTerm() int {
	return rf.logs[len(rf.logs)-1].Term
}

func (rf *Raft) getPrevLogIndex(server int) int{
	//只有leader调用
	return rf.nextIndex[server] - 1
}

func (rf *Raft) getPrevLogTerm(server int) int{
	//只有leader调用
	return rf.logs[rf.subIdx(rf.getPrevLogIndex(server))].Term
}

func (rf *Raft) subIdx(i int) int{
	return i - rf.lastIncludedIndex
}

func (rf *Raft) addIdx(i int) int{
	return i + rf.lastIncludedIndex
}

func (rf *Raft)checkState(role string, term int) bool{
	//检查server的状态是否与之前一致
	return rf.role == role && rf.currentTerm == term
}

//修改角色
//调用此函数的母函数一般已经拿到mu.lock()了
func (rf *Raft)convertRoleTo(role string){
	defer rf.persist()
	switch role {
	case Leader:
		WarnRaft.Printf("Raft:%2d term:%3d | %12s convert role to Leader!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Leader
		//初始化各个nextIndex数组
		for i := 0; i < len(rf.peers); i++{
			if i == rf.me{
				continue
			}
			rf.nextIndex[i] = len(rf.logs) + rf.lastIncludedIndex
			rf.matchIndex[i] = 0
		}
		//解决figure8，上线立刻同步一条信息
		//goroutine防止死锁
		//go func() {rf.Start("NULL")}()
	case Candidate:
		rf.currentTerm = rf.currentTerm + 1
		rf.votedFor = rf.me
		WarnRaft.Printf("Raft:%2d term:%3d | %12s convert role to Candidate!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Candidate
	case Follower:
		WarnRaft.Printf("Raft:%2d term:%3d | %12s convert role to Follower!\n", rf.me, rf.currentTerm, rf.role)
		rf.votedFor = votedNull
		rf.role = Follower
	}
}

func (rf *Raft) dropAndSet(ch chan bool){
	//排除管道内已有元素

	select {
	case <- ch:
	default:
	}
	ch <- true
}

//执行命令，提交数据
func (rf *Raft) applyLogs(){
	InfoRaft.Printf("Raft:%2d term:%3d | start apply log curCommit:%3d total:%3d!\n", rf.me, rf.currentTerm, rf.lastApplied, rf.commitIndex)
	//不能用goroutine，因为程序要求log按顺序交付
	for i := rf.lastApplied + 1; i <= rf.commitIndex; i++{
		//不行，lab 2B的测试不允许这么做
		//switch rf.logs[i].Command.(type) {
		//case string:
		//	if rf.logs[i].Command.(string) == "NULL"{
		//		//不执行，只是同步用
		//		continue
		//	}
		//default:
		//}
		rf.applyCh <- ApplyMsg{true, rf.logs[rf.subIdx(i)].Command, i, rf.currentTerm, rf.role}
		//InfoRaft.Printf("Raft:%2d term:%3d | %12v commit! CommitIndex:%3d total:%3d\n", rf.me ,rf.currentTerm, rf.role, i, rf.commitIndex)
	}
	InfoRaft.Printf("Raft:%2d term:%3d | Apply log {%4d => %4d} Done!\n", rf.me, rf.currentTerm, rf.lastApplied, rf.commitIndex)
	rf.lastApplied = rf.commitIndex
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

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
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

	//InfoKV.Printf("me:%2d | Start wait!\n", rf.me)
	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//InfoKV.Printf("me:%2d | start cmd!\n", rf.me)

	term = rf.currentTerm
	isLeader = rf.role == Leader
	if isLeader{
		//logs有一个占位符，所以其长度为3时，表明里面有2个命令，而新来的命令的提交index就是3,代表是第三个提交的。
		index = len(rf.logs) + rf.lastIncludedIndex
		rf.logs = append(rf.logs, Entries{rf.currentTerm, index, command})
		InfoRaft.Printf("Raft:%2d term:%3d | Leader receive a new command:%4v cmdIndex:%4v\n", rf.me, rf.currentTerm, command, index)

		rf.persist()
		}

	//InfoKV.Printf("me:%2d | start ok!\n", rf.me)
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
	rf.mu.Lock()
	WarnRaft.Printf("Sever index:[%3d]  Term:[%3d]  role:[%10s] has been killed.Turn off its log\n", rf.me, rf.currentTerm, rf.role)
	rf.mu.Unlock()
	rf.exitCh <- true
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
	rf.votedFor = votedNull

	rf.role = Follower

	//log下标从1开始，0是占位符，没有意义
	rf.logs = make([]Entries, 1)

	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	rf.appendCh = make(chan bool, 1)
	rf.voteCh = make(chan bool, 1)
	rf.exitCh = make(chan bool, 1)
	rf.leaderCh = make(chan bool, 1)

	rf.applyCh = applyCh

	rf.lastIncludedIndex = 0
	rf.lastIncludedTerm = -1

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	//logs[0]为占位符
	rf.logs[0] = Entries{rf.lastIncludedTerm, rf.lastIncludedIndex, -1}

	//要加这个，每次的rand才会不一样
	rand.Seed(time.Now().UnixNano())

	InfoRaft.Printf("Create a new Raft:[%3d]! term:[%3d]! Log length:[%4d]\n", rf.me,rf.currentTerm, rf.getLastLogIndex())

	//主程序负责创建raft实例、收发消息、模拟网络环境
	//每个实例在不同的goroutine里运行，模拟多台机器
	go func() {
		Loop:
			for{
				select{
				case <- rf.exitCh:
					break Loop
				default:
				}

				rf.mu.Lock()
				role := rf.role
				eTimeout := rf.electionTimeout()
				//InfoRaft.Printf("Raft:%2d term:%3d | role:%10s timeout:%v\n", rf.me, rf.currentTerm, rf.role, eTimeout)
				rf.mu.Unlock()

				switch role{
				case Leader:
					rf.broadcastEntries()
					time.Sleep(heartBeat * time.Millisecond)
				case Candidate:
					go rf.leaderElection()
					select{
					//在request和append里已经修改角色为follower了
					case <- rf.appendCh:
					case <- rf.voteCh:
					case <- rf.leaderCh:
					case <- time.After(eTimeout):
						rf.mu.Lock()
						rf.convertRoleTo(Candidate)
						rf.mu.Unlock()
					}
				case Follower:
					select{
					case <- rf.appendCh:
					case <- rf.voteCh:
					case <- time.After(eTimeout):
						rf.mu.Lock()
						rf.convertRoleTo(Candidate)
						rf.mu.Unlock()
					}
				}
			}

	}()

	return rf
}

func (rf *Raft) leaderElection(){
	//candidate竞选leader
	rf.mu.Lock()
	//candidate term已经在changeRole里+1了
	//发给每个节点的请求参数都是一样的。
	requestArgs := &RequestVoteArgs{rf.currentTerm, rf.me, rf.getLastLogIndex(), rf.getLastLogTerm()}
	rf.mu.Unlock()

	voteCnt := 1 //获得的选票，自己肯定是投给自己啦
	voteFlag := true //收到过半选票时管道只通知一次
	voteL := sync.Mutex{}

	for followerId, _ := range rf.peers{
		if followerId == rf.me{
			continue
		}

		rf.mu.Lock()
		if !rf.checkState(Candidate, requestArgs.Term){
			//发送大半投票时，发现自己已经不是candidate了
			rf.mu.Unlock()
			return
		}
		rf.mu.Unlock()

		go func(server int) {
			reply := &RequestVoteReply{}
			if ok := rf.sendRequestVote(server, requestArgs, reply); ok{
				//ok仅仅代表得到回复，
				//ok==false代表本次发送的消息丢失，或者是回复的信息丢失

				rf.mu.Lock()
				defer rf.mu.Unlock()
				if reply.Term > rf.currentTerm{
					//有更高term的存在
					rf.convertRoleTo(Follower)
					return
				}

				if !rf.checkState(Candidate, requestArgs.Term){
					//收到投票时，已经不是candidate了
					return
				}

				if reply.VoteGranted{
					//收到投票
					voteL.Lock()
					defer voteL.Unlock()
					voteCnt = voteCnt + 1
					if voteFlag && voteCnt > len(rf.peers)/2{
						voteFlag = false
						rf.convertRoleTo(Leader)
						rf.dropAndSet(rf.leaderCh)
					}
				}

			}
		}(followerId)
	}
}

func (rf *Raft) broadcastEntries() {
	//leader广播日志
	rf.mu.Lock()
	curTerm := rf.currentTerm
	rf.mu.Unlock()

	commitFlag := true //超半数commit只修改一次leader的logs
	commitNum := 1     //记录commit某个日志的节点数量
	commitL := sync.Mutex{}

	for followerId, _ := range rf.peers {
		if followerId == rf.me {
			continue
		}

		//发送信息
		go func(server int) {
			for {
				//for循环仅针对leader和follower的日志不匹配而需要重新发送日志的情况
				//其他情况直接返回

				//每一个节点的请求参数都不一样
				rf.mu.Lock()
				if !rf.checkState(Leader, curTerm) {
					//已经不是leader了
					rf.mu.Unlock()
					return
				}

				//脱离集群很久的follower回来，nextIndex已经被快照了
				//先判断nextIndex是否大于rf.lastIncludedIndex
				next := rf.nextIndex[server]
				if next <= rf.lastIncludedIndex{
					//注意，此时持有rf.mu锁
					rf.sendSnapshot(server)
					return
				}

				appendArgs := &AppendEntriesArgs{curTerm,
					rf.me,
					rf.getPrevLogIndex(server),
					rf.getPrevLogTerm(server),
					rf.logs[rf.subIdx(next):],
					rf.commitIndex}

				rf.mu.Unlock()
				reply := &AppendEntriesReply{}

				ok := rf.sendAppendEntries(server, appendArgs, reply);

				if !ok{
					return
				}

				rf.mu.Lock()
				if reply.Term > curTerm {
					//返回的term比发送信息时leader的term还要大
					rf.currentTerm = reply.Term
					rf.convertRoleTo(Follower)
					InfoRaft.Printf("Raft:%2d term:%3d | leader done! become follower\n", rf.me, rf.currentTerm)
					rf.mu.Unlock()
					return
				}

				//发送信息可能很久，所以收到信息后需要确认状态
				//类似于发送一条信息才同步
				//一个新leader刚开始发送心跳信号，即便和follower不一样也不修改follower的日志
				//只有当新Leader接收新消息时，才会修改follower的值，防止figure8的情况发生
				if !rf.checkState(Leader, curTerm) || len(appendArgs.Entries) == 0{
					//如果server当前的term不等于发送信息时的term
					//表明这是一条过期的信息，不要了

					//或者是心跳信号且成功，也直接返回
					//如果是心跳信号，但是append失败，有可能是联系到脱离集群很久的节点，需要更新相应的nextIndex
					//比如一个新leader发送心跳信息，如果一个follower的日志比args.prevLogIndex小
					//那么此时reply失败，需要更新nextIndex
					rf.mu.Unlock()
					return
				}

				if reply.Success {
					//append成功

					//考虑一种情况
					//第一个日志长度为A，发出后，网络延迟，很久没有超半数commit
					//因此第二个日志长度为A+B，发出后，超半数commit，修改leader
					//这时第一次修改的commit来了，因为第二个日志已经把第一次的日志也commit了
					//所以需要忽略晚到的第一次commit
					curCommitLen := appendArgs.PrevLogIndex +  len(appendArgs.Entries)

					if curCommitLen < rf.commitIndex{
						rf.mu.Unlock()
						return
					}

					//两者相等的时候
					//代表follower的日志长度==match的长度，所以nextIndex需要+1
					if curCommitLen >= rf.matchIndex[server]{
						rf.matchIndex[server] = curCommitLen
						rf.nextIndex[server] = rf.matchIndex[server] + 1
					}

					commitL.Lock()
					defer commitL.Unlock()

					commitNum = commitNum + 1
					if commitFlag && commitNum > len(rf.peers)/2 {
						//第一次超半数commit
						commitFlag = false

						//leader提交日志，并且修改commitIndex
						/**
						if there exists an N such that N >  commitIndex, a majority
						of matchIndex[N] >= N, and log[N].term == currentTerm
						set leader's commitIndex = N
						如果在当前任期内，某个日志已经同步到绝大多数的节点上，
						并且日志下标大于commitIndex，就修改commitIndex。
						*/

						rf.commitIndex = curCommitLen
						rf.applyLogs()
					}

					rf.mu.Unlock()
					return

				} else {
					//prevLogIndex or prevLogTerm不匹配

					//如果leader没有conflictTerm的日志，那么重新发送所有日志

					//新加入的节点可能没有日志，其conflitIndex是0

					if reply.ConflictTerm == -1{
						//follower缺日志 或者 上一次发送的日志follower已经快照了
						rf.nextIndex[server] = reply.ConflictIndex
					}else{
						//日志不匹配
						rf.nextIndex[server] = rf.addIdx(1)
						i := reply.ConflictIndex
						for ; i > rf.lastIncludedIndex; i--{
							if rf.logs[rf.subIdx(i)].Term == reply.ConflictTerm{
								rf.nextIndex[server] = i + 1
								break
							}
						}
						if i <= rf.lastIncludedIndex && rf.lastIncludedIndex != 0{
							//leader拥有的日志都不能与follwer匹配
							//需要发送快照
							rf.nextIndex[server] = rf.lastIncludedIndex
						}
					}

					InfoRaft.Printf("Raft:%2d term:%3d | Msg to %3d fail,decrease nextIndex to:%3d\n",
						rf.me, rf.currentTerm, server, rf.nextIndex[server])
					rf.mu.Unlock()
				}
			}

		}(followerId)

	}
}

//===============================================================================================================
//lab3添加的代码如下
//snapshot
//===============================================================================================================
func (rf *Raft) TakeSnapshot(rawSnapshot []byte, appliedId int, term int){
	//data kv需要快照的数据，index，快照对应的日志下标，term，下标所属term
	rf.mu.Lock()
	defer rf.mu.Unlock()
	InfoKV.Printf("Raft:%2d term:%3d | Begin snapshot! appliedId:%4d term:%4d lastIncludeIndex:%4d\n", rf.me, rf.currentTerm, appliedId, term, rf.lastIncludedIndex)

	if appliedId <= rf.lastIncludedIndex{
		//忽略发起的旧快照
		//在一次apply中，由于rf.mu的缘故，会并发发起多个快照操作
		return
	}

	logs := make([]Entries, 0)
	//此时logs[0]是快照对应的最后一个日志，是一个占位符。
	logs = append(logs, rf.logs[rf.subIdx(appliedId):]...)

	rf.logs = logs
	rf.lastIncludedTerm = term
	rf.lastIncludedIndex = appliedId
	rf.persistStateAndSnapshot(rawSnapshot)
}

func (rf *Raft) persistStateAndSnapshot(snapshot []byte){
	w := new(bytes.Buffer)
	enc := labgob.NewEncoder(w)
	enc.Encode(rf.currentTerm)
	enc.Encode(rf.votedFor)
	enc.Encode(rf.logs)
	enc.Encode(rf.lastIncludedIndex)
	enc.Encode(rf.lastIncludedTerm)
	raftState := w.Bytes()
	rf.persister.SaveStateAndSnapshot(raftState, snapshot)
}

type InstallSnapshotArgs struct {
	Term 	int //leader's term
	LeaaderId 	int
	LastIncludedIndex	int
	LastIncludedTerm	int
	Data 	[]byte //snapshot
}

type InstallSnapshotReply struct{
	Term 	int
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply){
	InfoKV.Printf("Raft:%2d term:%3d | receive snapshot from leader:%2d ", rf.me, rf.currentTerm, args.LeaaderId)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	if rf.currentTerm > args.Term || args.LastIncludedIndex <= rf.lastIncludedIndex{
		InfoKV.Printf("Raft:%2d term:%3d | stale snapshot from leader:%2d | me:{index%4d term%4d} leader:{index%4d term%4d}",
			rf.me, rf.currentTerm, args.LeaaderId, rf.lastIncludedIndex, rf.lastIncludedTerm, args.LastIncludedIndex, args.LastIncludedTerm)
		return
	}

	//InfoKV.Printf("Raft:%2d term:%3d | install snapshot from leader:%2d ", rf.me, rf.currentTerm, args.LeaaderId)

	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term
		rf.convertRoleTo(Follower)
	}

	rf.dropAndSet(rf.appendCh)

	logs := make([]Entries, 0)
	if args.LastIncludedIndex <= rf.getLastLogIndex() {
		logs = append(logs, rf.logs[rf.subIdx(args.LastIncludedIndex):]...)
	}else{
		logs = append(logs, Entries{args.LastIncludedTerm,args.LastIncludedIndex,-1})
	}
	rf.logs = logs

	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludedTerm = args.LastIncludedTerm


	rf.lastApplied = max(rf.lastIncludedIndex, rf.lastApplied)
	rf.commitIndex = max(rf.lastIncludedIndex, rf.commitIndex)

	rf.persistStateAndSnapshot(args.Data)

	msg := ApplyMsg{
		false,
		args.Data,
		rf.lastIncludedIndex,
		rf.lastIncludedTerm,
		rf.role,
	}

	rf.applyCh <- msg

	InfoKV.Printf("Raft:%2d term:%3d | Install snapshot Done!\n", rf.me, rf.currentTerm)

}

func (rf *Raft) sendSnapshot(server int) {
	InfoKV.Printf("Raft:%2d term:%3d | Leader send snapshot{index:%4d term:%4d} to follower %2d\n", rf.me, rf.currentTerm, rf.lastIncludedIndex, rf.lastIncludedTerm, server)
	//leader发送快照逻辑

	//进来时拥有rf.mu.lock
	//只在一个地方进来
	arg := InstallSnapshotArgs{
		rf.currentTerm,
		rf.me,
		rf.lastIncludedIndex,
		rf.lastIncludedTerm,
		rf.persister.ReadSnapshot(),
	}
	rf.mu.Unlock()

	repCh := make(chan struct{})
	reply := InstallSnapshotReply{}

	go func() {
		if ok := rf.peers[server].Call("Raft.InstallSnapshot", &arg, &reply); ok{
			repCh <- struct{}{}
		}
	}()

	select{
	case <- time.After(RPC_CALL_TIMEOUT):
		InfoKV.Printf("Raft:%2d term:%3d | Timeout! Leader send snapshot to follower %2d failed\n", arg.LeaaderId, arg.Term, server)
		return
	case <- repCh:
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if reply.Term > rf.currentTerm{
		//follower的term比自己大
		rf.currentTerm = reply.Term
		rf.convertRoleTo(Follower)
		return
	}

	if !rf.checkState(Leader, arg.Term){
		//rpc回来后不再是leader
		return
	}
	//快照发送成功
	rf.nextIndex[server] = arg.LastIncludedIndex + 1
	rf.matchIndex[server] = arg.LastIncludedIndex

	//InfoKV.Printf("Raft:%2d term:%3d | OK! Leader send snapshot to follower %2d\n", rf.me, rf.currentTerm, server)
}