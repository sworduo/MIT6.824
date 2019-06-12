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
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

//term
const(
	Leader = "Leader"
	Candidate = "Candidate"
	Follower = "Follower"
	votedNull = -1 //本轮没有投票给任何人
	heartBeat = time.Duration(100) //leader的心跳时间
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
	//DPrintf(rf.log, "warn", "Server:%3d role:%12s isleader:%t\n", rf.me, rf.role, isleader)
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
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
	Info.Printf("me:%2d term:%3d | Role:%10s Persist data! VotedFor:%3d len(Logs):%3d\n",
		rf.me, rf.currentTerm, rf.role, rf.votedFor, len(rf.logs))
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

	//还没运行之前调用此函数
	//所以不用加锁了吧
	if dec.Decode(&term) != nil || dec.Decode(&votedFor) !=nil || dec.Decode(&logs) != nil{
		Info.Printf("me:%2d term:%3d | Failed to read persist data!\n")
	}else{
		rf.currentTerm = term
		rf.votedFor = votedFor
		rf.logs = logs
		Info.Printf("me:%2d term:%3d | Read persist data successful! VotedFor:%3d len(Logs):%3d\n",
			rf.me, rf.currentTerm, rf.votedFor, len(rf.logs))
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

//leader调用follower的AppendEntries RPC服务
//站在follower角度完成下面这个RPC调用
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	//follower收到leader的信息
	rf.mu.Lock()
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
	log_less := rf.getLastLogIndex() < args.PrevLogIndex
	//接收者日志大于等于leader发来的日志  且 日志项不匹配
	log_dismatch := !log_less  && rf.logs[args.PrevLogIndex].Term != args.PrevLogTerm

	if log_less{
		//如果follower日志较少
		reply.ConflictTerm = rf.getLastLogTerm()
		reply.ConflictIndex = rf.getLastLogIndex()
		Info.Printf("me:%2d term:%3d | receive leader:[%3d] message but lost any message!\n", rf.me, rf.currentTerm, args.LeaderId)
	} else if log_dismatch{
		//日志项不匹配，找到follower属于这个term的第一个日志，方便回滚。
		reply.ConflictTerm = rf.logs[args.PrevLogIndex].Term
		for i := args.PrevLogIndex; i > 0 ; i--{
			if rf.logs[i].Term != reply.ConflictTerm{
				break
			}
			reply.ConflictIndex = i
		}
		Info.Printf("me:%2d term:%3d | receive leader:[%3d] message but not match!\n", rf.me, rf.currentTerm, args.LeaderId)
	} else {
		//接收者的日志大于等于prevlogindex，且在prevlogindex处日志匹配
		rf.currentTerm = args.Term
		reply.Success = true

		//修改日志长度
		//找到接收者和leader（如果有）第一个不相同的日志
		leng := min(rf.getLastLogIndex()-args.PrevLogIndex, len(args.Entries))
		i := 0
		for ; i < leng; i++ {
			if rf.logs[args.PrevLogIndex+i+1].Term != args.Entries[i].Term {
				rf.logs = rf.logs[:args.PrevLogIndex+i+1]
				break
			}
		}
		if i != len(args.Entries) {
			rf.logs = append(rf.logs, args.Entries[i:]...)
		}

		if len(args.Entries) != 0{
			//心跳信号不输出
			//心跳信号可能会促使follower执行命令

			//心跳信号没有修改votedFor、log和currentTerm
			rf.persist()
			Info.Printf("me:%2d term:%3d | receive new command:%3d index:%4d from leader:%3d, size:%3d\n",
				rf.me, rf.currentTerm, rf.logs[rf.getLastLogIndex()].Command, rf.getLastLogIndex(), args.LeaderId, args.PrevLogIndex + len(args.Entries) - rf.commitIndex)
		}

		//修改commitIndex
		if args.LeaderCommit > rf.commitIndex {
			rf.commitIndex = min(args.LeaderCommit, len(rf.logs)-1)
			rf.applyLogs()
			}

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
	//回复的信息可能会丢失，所以需要加上另一个判断
	voteOrNot := rf.votedFor == votedNull || rf.votedFor == args.CandidateId

	if  newerEntries && voteOrNot{
			rf.votedFor = args.CandidateId
			reply.VoteGranted = true

			rf.dropAndSet(rf.voteCh)
			Warn.Printf("me:%2d term:%3d | vote to candidate %3d\n", rf.me, rf.currentTerm, args.CandidateId)
			rf.persist()
	}
}

func (rf *Raft) getLastLogIndex() int {
	//logs下标从1开始，logs[0]是占位符
	return len(rf.logs) - 1
}

func (rf *Raft) getLastLogTerm() int {
	return rf.logs[rf.getLastLogIndex()].Term
}

func (rf *Raft) getPrevLogIndex(server int) int{
	//只有leader调用
	return rf.nextIndex[server] - 1
}

func (rf *Raft) getPrevLogTerm(server int) int{
	//只有leader调用
	return rf.logs[rf.getPrevLogIndex(server)].Term
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
		Warn.Printf("me:%2d term:%3d | %12s convert role to Leader!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Leader
		//初始化各个nextIndex数组
		for i := 0; i < len(rf.peers); i++{
			if i == rf.me{
				continue
			}
			rf.nextIndex[i] = len(rf.logs)
			rf.matchIndex[i] = 0
		}
	case Candidate:
		rf.currentTerm = rf.currentTerm + 1
		rf.votedFor = rf.me
		Warn.Printf("me:%2d term:%3d | %12s convert role to Candidate!\n", rf.me, rf.currentTerm, rf.role)
		rf.role = Candidate
	case Follower:
		Warn.Printf("me:%2d term:%3d | %12s convert role to Follower!\n", rf.me, rf.currentTerm, rf.role)
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
	//不能用goroutine，因为程序要求log按顺序交付
	for i := rf.lastApplied + 1; i <= rf.commitIndex; i++{
		rf.applyCh <- ApplyMsg{true, rf.logs[i].Command, i}
	}
	rf.lastApplied = rf.commitIndex
	Info.Printf("me:%2d term:%3d | %12v commit! cmd:%3d CommitIndex:%3d\n",
		rf.me ,rf.currentTerm, rf.role, rf.logs[rf.commitIndex].Command, rf.commitIndex)
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

	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	term = rf.currentTerm
	isLeader = rf.role == Leader
	if isLeader{
		//logs有一个占位符，所以其长度为3时，表明里面有2个命令，而新来的命令的提交index就是3,代表是第三个提交的。
		index = len(rf.logs)
		rf.logs = append(rf.logs, Entries{rf.currentTerm, index, command})
		Info.Printf("me:%2d term:%3d | Leader receive a new command:%3d\n", rf.me, rf.currentTerm, command.(int))

		rf.persist()
		}

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
	Warn.Printf("Sever index:[%3d]  Term:[%3d]  role:[%10s] has been killed.Turn off its log\n", rf.me, rf.currentTerm, rf.role)
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
	//term为-1,表示第一个leader的term编号是0,test只接受int型的command
	rf.logs[0] = Entries{-1, 0, -1}
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	rf.appendCh = make(chan bool, 1)
	rf.voteCh = make(chan bool, 1)
	rf.exitCh = make(chan bool, 1)
	rf.leaderCh = make(chan bool, 1)

	rf.applyCh = applyCh

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	//要加这个，每次的rand才会不一样
	rand.Seed(time.Now().UnixNano())

	Info.Printf("Create a new server:[%3d]! term:[%3d]! Log length:[%4d]\n", rf.me,rf.currentTerm, rf.getLastLogIndex())

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
				//Info.Printf("me:%2d term:%3d | role:%10s timeout:%v\n", rf.me, rf.currentTerm, rf.role, eTimeout)
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

		if !rf.checkState(Candidate, requestArgs.Term){
			//发送大半投票时，发现自己已经不是candidate了
			return
		}

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
				appendArgs := &AppendEntriesArgs{curTerm,
					rf.me,
					rf.getPrevLogIndex(server),
					rf.getPrevLogTerm(server),
					rf.logs[rf.nextIndex[server]:],
					rf.commitIndex}

				rf.mu.Unlock()
				reply := &AppendEntriesReply{}

				ok := rf.sendAppendEntries(server, appendArgs, reply);

				if !ok{
					return
				}

				rf.mu.Lock()

				//发送信息可能很久，所以收到信息后需要确认状态
				if !rf.checkState(Leader, curTerm) || len(appendArgs.Entries) == 0 {
					//如果server当前的term不等于发送信息时的term
					//表明这是一条过期的信息，不要了

					//或者是心跳信号，也直接返回
					rf.mu.Unlock()
					return
				}

				if reply.Term > curTerm {
					//返回的term比发送信息时leader的term还要大
					rf.currentTerm = reply.Term
					rf.convertRoleTo(Follower)
					rf.mu.Unlock()
					Info.Printf("me:%2d term:%3d | leader done done! become follower\n", rf.me, rf.currentTerm)
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

					if curCommitLen <= rf.commitIndex{
						rf.mu.Unlock()
						return
					}

					if curCommitLen > rf.matchIndex[server]{
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
					rf.nextIndex[server] = 1
					for i := reply.ConflictIndex; i > 0; i--{
						if rf.logs[i].Term == reply.ConflictTerm{
							rf.nextIndex[server] = i + 1
							break
						}
					Info.Printf("me:%2d term:%3d | Msg to %3d append fail,decrease nextIndex to:%3d\n",
						rf.me, rf.currentTerm, server, rf.nextIndex[server])
					}
					rf.mu.Unlock()
				}
			}

		}(followerId)

	}
}

