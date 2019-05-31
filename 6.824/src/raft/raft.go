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
	Leader = iota //0
	Candidate //1
	Follower //2
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
	role 	int //Leader or candidate or follower
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

	log 	bool //是否输出这一个raft实例的log

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
	DPrintf(rf.log, "info", "Server:%3d role:%3d  Leader is %3d  isleader:%t\n", rf.me, rf.role, Leader, isleader)
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
type AppendEntriesReply struct{
	Term 	int //接收到信息的follower的currentTerm，方便过期leader更新信息。
	Success 	bool // true if follower contained entry matching prevLogIndex and PrevLogTerm
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

	//过期leader的信息不能作为本次term的heartbeat
	if args.Term < rf.currentTerm{
		reply.Success = false
		reply.Term = rf.currentTerm
		return
	}


	rf.currentTerm = args.Term
	go func() {rf.appendCh <- args}()

	if len(rf.logEntries) == 1{
		//此时rf中并没有任何日志
		reply.Success = true
		reply.Term = rf.currentTerm
		rf.logEntries = append(rf.logEntries, args.Entries...)
		//leader发来的日志可能包含还未得到超半数follower commit的日志
		rf.commitIndex = min(len(args.Entries) + rf.commitIndex, args.LeaderCommit)
		return
	}else if len(rf.logEntries)-1 < args.PrevLogIndex || rf.logEntries[args.PrevLogIndex].Term != args.PrevLogTerm{
		//logEntries下标从1开始，所以其真正日志数量=len(logEntries)-1
		//follower的日志长度小于prevLogIndex时，需要减小prevLogIndex
		DPrintf(rf.log, "warn", "leader prevlogterm:%3d,   server last entries:%3d", args.PrevLogTerm, rf.logEntries[args.PrevLogIndex].Term)
		reply.Success = false
		reply.Term = rf.currentTerm
		return
	}

	reply.Success = true
	reply.Term = rf.currentTerm
	if len(args.Entries) == 0{
		//心跳信号直接返回
		return
	}

	//length := min(len(rf.logEntries) - 1 - args.PrevLogIndex, len(args.Entries))
	//i := 0
	//for ; i < length; i++{
	//	if rf.logEntries[i+1+args.PrevLogIndex].Term != args.Term{
	//		break
	//	}
	//}
	//
	//
	//if i == len(args.Entries){
	//	//表明本follower的日志要多于args的日志
	//	rf.logEntries = rf.logEntries[:args.PrevLogIndex+i+1]
	//}else if i == len(rf.logEntries) - 1 - args.PrevLogIndex{
	//	//表明args的日志多余follower的日志
	//	rf.logEntries = append(rf.logEntries, args.Entries...)
	//}else{
	//	//args的日志和follower的日志有部分相同，部分不同
	//
	//}


	//上面的if else之后，能来到这里，意味着follower在args.prevLogIndex之前和leader的日志相同
	//剩下几种情况：
	//follower在prevLogIndex之后的日志长度小于args.Entries，且重叠部分相同
	//follower在prevLogIndex之后的日志长度长于args.Entries，且重叠部分相同
	//follower在prevLogIndex之后的日志长度小于args.Entries，且重叠部分部分相同，部分不同
	//follower在prevLogIndex之后的日志长度长于args.Entries，且重叠部分部分相同，部分不同
	//不管是哪一种，都需要rf.logEntries拼接上args的新日志，那我直接拼接就行了
	//缺点：如果args.entries过大，切片复制挺花时间的
	DPrintf(rf.log, "warn", "server:[%3d] modified its logEntries!\n", rf.me)
	rf.logEntries = rf.logEntries[:args.PrevLogIndex+1]
	rf.logEntries = append(rf.logEntries, args.Entries...)
	//leader发来的日志可能包含还未得到超半数follower commit的日志
	if args.LeaderCommit > rf.commitIndex{
		rf.commitIndex = min(len(args.Entries)-1, args.LeaderCommit)
	}
	return
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

	if rf.currentTerm == args.Term && rf.votedFor != -1 && rf.votedFor != args.CandidateId{
		//票已经投给同一term的其他candidate了
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
	}else if rf.currentTerm > args.Term{
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
	}else if rf.logEntries[rf.commitIndex].Term > args.LastLogTerm{
		//由于candidate投票时自己的term会+1
		//所以一般follower term < candidate term
		//先判断term，再判断index，顺序不能错
		//因为有可能出现follower term < candidate term && follower index > candidate index的情况
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
	}else if rf.logEntries[len(rf.logEntries)-1].Term == args.LastLogTerm && len(rf.logEntries)-1 > args.LastLogIndex{
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
	}else{
		//follower投票给符合条件的candidaet
		//或者是term较低的candidate投票给term较高的candidate
		rf.currentTerm = args.Term
		rf.votedFor = args.CandidateId
		reply.Term = rf.currentTerm
		reply.VoteGranted = true
		//重启定时器
		go func() {rf.voteCh <- args}()
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
	DPrintf(rf.log, "info", "Sever index:[%3d]  Term:[%3d]  has been killed.Turn off its log\n", rf.me, rf.currentTerm)
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
	rf.log = true

	//要加这个，每次的rand才会不一样
	rand.Seed(time.Now().UnixNano())

	DPrintf(rf.log, "info", "Create a new server:[%3d]! term:[%3d]\n", rf.me,rf.currentTerm)
	//一个raft实例在一台机器上初始化后，就要开始竞选leader，或者接收leader的指令
	//用goroutine来新建raft实例
	//因为这是在一台机器上模拟多机器，不是真的在多机器上各自创建实例
	go rf.runRaft(applyCh)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	return rf
}

func (rf *Raft) runRaft(applyCh chan ApplyMsg){
	//随机时间计算逻辑:election timeout = basicTime + rand.Intn(randTime)
	//因为rand.Intn(n)是随机输出0-(n-1)的数字
	//假如我们希望的随机时间可能是500ms-650ms
	//则timeout = 500 + randIntn(650)

	//timeout不能太小，否则会频繁触发leader选举
	//timeout也不能太大，因为要在5s之内检测leader失败并且完成新一轮选举
	//心跳信号100ms一次
	//timeout = basictime + randtime*rand.Intn(randNum)
	//这样一来区分度大一点。
	//主机比较少时，可以这么做，主机数量多不能这么做,数量一多，随机到同一时间的多，就丧失随机性了
	basicTime := 400
	randTime := 5
	randNum := 30
	for{
		rf.mu.Lock()
		switch rf.role {
		case Leader:
			rf.mu.Unlock()
			rf.leader(applyCh)
		case Candidate:
			rf.mu.Unlock()
			rf.candidate(basicTime, randTime, randNum)
		case Follower:
			rf.mu.Unlock()
			rf.follower(basicTime, randTime, randNum)
		}
	}
}

//when this raft peer is a leader
func (rf *Raft) leader(applyCh chan ApplyMsg){

	//记录发送消息时leader的term
	//否则由于消息延迟，消息回来时leader可能已经不是leader了
	//并且回来时leader的term可能会被新leader的term覆盖了
	//如果是在goroutine里申请curTerm变量。
	//那么可能for循环中，前一次term还是这个，后一次term可能就被新leader覆盖了
	//当term改变时，代表本leader不是leader了，应该退出
	rf.mu.Lock()
	curTerm := rf.currentTerm
	rf.mu.Unlock()

	for followerId, _ := range rf.peers{
		if followerId == rf.me{
			continue
		}
		rf.nextIndex[followerId] = len(rf.logEntries)
		rf.matchIndex[followerId] = 0
	}
	for{
		//用来判断某个日志是否超半数机器commit
		commitL := sync.Mutex{}
		commitNum := 1
		commitFlag := 0

		for followerId, _ := range rf.peers{
			if followerId == rf.me{
				continue
			}
			//因为参数每次都需要更新
			//所以必须在for循环里定义
			aeArgs := &AppendEntriesArgs{}

			rf.mu.Lock()
			aeArgs.Term = rf.currentTerm
			aeArgs.LeaderId = rf.me
			aeArgs.PrevLogIndex = rf.nextIndex[followerId] - 1
			//DPrintf(rf.log, "info", "leader:[%3d]   follower:[%3d]  prevLogIndex:%3d", rf.me, followerId, aeArgs.PrevLogIndex)
			aeArgs.PrevLogTerm = rf.logEntries[aeArgs.PrevLogIndex].Term
			aeArgs.Entries = make([]Entries, 0)
			aeArgs.LeaderCommit = rf.commitIndex
			rf.mu.Unlock()

			go func(server int, args *AppendEntriesArgs) {
				//在go routine里定义返回值，防止多个goroutine绑定到同一个返回值里
				reply := &AppendEntriesReply{}
				DPrintf(rf.log, "warn", "Leader:[%3d] send message to server:[%3d]\n", rf.me, server)
				//call的返回值ok==true，只能表明有信息返回
				if ok := rf.peers[server].Call("Raft.AppendEntries", args, reply); ok {
					rf.mu.Lock()
					defer rf.mu.Unlock()

					if rf.role != Leader || rf.currentTerm != curTerm{
						//忽略掉之前当leader时发出的信息
						//多种可能：
						//此时是follower，此时是leader，此时经过新一轮选举成为leader
						//直接忽略过期的信息
						return
					}

					if reply.Success{
						commitL.Lock()
						defer commitL.Unlock()
						commitNum = commitNum + 1
						if commitNum > len(rf.peers)/2 && commitFlag == 0{
							//自己必然commit，所以共5台机器，只需要额外再拿2个commit就能超半数
							//只提交一次管道，后面不再提交，防止goroutine阻塞
							//更新自己的日志后commit
							commitFlag = 1
							rf.commitIndex = rf.commitIndex + len(args.Entries)
							rf.logEntries = append(rf.logEntries, args.Entries...)
							go func(command interface{}, commitIndex int) {
								applyCh <- ApplyMsg{true, command, commitIndex}
								}(rf.logEntries[len(rf.logEntries)-1].Command, rf.commitIndex)
						}
					}else{
						//appendEntries失败
						//两种可能
						//1.本leader已经过期
						//2.follower的日志不匹配
						if reply.Term > curTerm{
							//leader转为follower
							DPrintf(rf.log, "info", "Leader:[%3d] old term:[%3d]  new term [%3d] become follower!\n", rf.me, curTerm, reply.Term)
							rf.currentTerm = reply.Term
							rf.role = Follower
						}else{
							rf.nextIndex[server] = rf.nextIndex[server] - 1
							DPrintf(rf.log, "info", "leader:%3d server:%3d nextIndex:%3d", rf.me, server, rf.nextIndex[server])
							DPrintf(rf.log, "info", "reply.term:[%3d] leader curTrem:[%3d]\n", reply.Term, curTerm)
						}
					}
				}else{
					rf.mu.Lock()
					DPrintf(rf.log, "warn", "Term:[%3d] Message from leader:[%3d] to follower:[%3d] is missed!\n", rf.currentTerm, rf.me, server)
					rf.mu.Unlock()
				}
			}(followerId, aeArgs)
		}
		select {
		case args := <- rf.appendCh:
			//收到新leader的信息
			rf.mu.Lock()
			DPrintf(rf.log, "info", "Leader:[%3d] old term:[%3d] drop to follower! New leader:[%3d] new term:[%3d]\n", rf.me, curTerm, args.LeaderId, args.Term)
			rf.role = Follower
			rf.mu.Unlock()
			return
		case <- time.After(100 * time.Millisecond):
			//每隔100ms发送一次心跳信号
		}
	}

}

//when this raft peer is a candidate
func (rf *Raft) candidate(basicTime, randTime, randNum int){

	candidateL := sync.Mutex{}
	candidateOk := make(chan bool)


	//election timeout记得每次更新
	timeout := basicTime + randTime * rand.Intn(randNum)
	//DPrintf(rf.log, "warn", "role: [%2d] peer: [%3d]  term: [%3d]  timeout: [%4d] ms\n", rf.role, rf.me, rf.currentTerm, timeout)

	voteArg := &RequestVoteArgs{}

	rf.mu.Lock()
	//每次投票自增
	rf.currentTerm = rf.currentTerm + 1
	rf.votedFor = rf.me
	voteArg.Term = rf.currentTerm
	voteArg.CandidateId = rf.me
	voteArg.LastLogIndex = len(rf.logEntries) - 1
	voteArg.LastLogTerm = rf.logEntries[voteArg.LastLogIndex].Term
	DPrintf(rf.log, "info", "candidate:[%3d] term:[%3d] timeout:[%4dms] begin to vote!\n", rf.me, rf.currentTerm, timeout)
	rf.mu.Unlock()

	//每次竞选重新设置voteCnt,leaderMsg，防止上一个election的干扰
	voteCnt := 1 //投自己一票
	leaderMsg := 0


	for followerId, _ := range rf.peers{
		if followerId == rf.me{
			continue
		}

		go func(server int) {
			//在goroutine里定义返回参数
			//因为是引用传递，如果在for外面定义，那么所有的返回值都将引用到同一个值
			//vote参数可以在外面定义，因为请求投票参数每次都不变的
			voteReply := &RequestVoteReply{}

			//call的返回值ok==true，只能表明有信息返回
			if ok := rf.sendRequestVote(server, voteArg, voteReply); ok {
				if voteReply.VoteGranted {
					//收到选票
					candidateL.Lock()
					defer candidateL.Unlock()
					voteCnt = voteCnt + 1
					if voteCnt > len(rf.peers)/2 && leaderMsg == 0 {
						//自己必然投自己一票，所以共5台机器，再额外拿2票即可
						//所以candidateOk这个管道只会发送一次信息
						//后续的goroutine不会再发送信息了，以免管道阻塞，goroutine结束不了
						leaderMsg = 1
						candidateOk <- true
					}
				} else {
					//没有得到该follower的选票
					//不需要做任何事情
				}
			}else{
				//发送的信息没有得到任何回复
				//啥都不做算了
				rf.mu.Lock()
				DPrintf(rf.log,"warn", "Term:[%3d] message from candidate:[%3d] to follower:[%3d] is lost!\n", rf.currentTerm, rf.me, server)
				rf.mu.Unlock()
			}
		}(followerId)
	}

	select{
		case args := <- rf.appendCh:
			//当收到新的leader的信息时，将自己变为follower
			rf.mu.Lock()
			rf.role = Follower
			DPrintf(rf.log, "info", "Candidate:[%3d] term:[%3d] convert to follower! New leader:[%3d]\n", rf.me, rf.currentTerm, args.LeaderId)
			rf.mu.Unlock()
		case args := <- rf.voteCh:
			rf.mu.Lock()
			rf.role = Follower
			DPrintf(rf.log, "info", "Candidate:[%3d] vote to another candidate:[%3d] term:[%3d]\n", rf.me, args.CandidateId, args.Term)
			rf.mu.Unlock()
		case <- candidateOk:
			//超半数承认，则将自己变为leader
			rf.mu.Lock()
			rf.role = Leader
			DPrintf(rf.log, "info", "Candidate:[%3d] term:[%3d] become new leader!\n", rf.me, rf.currentTerm)
			rf.mu.Unlock()
		case <- time.After(time.Duration(timeout) * time.Millisecond):
			//本轮超时，降身份为follower，重新选举
			rf.mu.Lock()
			rf.role = Follower
			DPrintf(rf.log, "info", "Candidate:[%3d] term:[%3d] timeout! Back to follower. Start a new election!\n", rf.me, rf.currentTerm)
			rf.mu.Unlock()
	}


}

//when this raft peer is a follower
func (rf *Raft) follower(basicTime, randTime, randNum int){
	for{
		//election timeout记得每次更新
		timeout := basicTime + randTime * rand.Intn(randNum)
		//DPrintf(rf.log, "warn", "role: [%2d] peer: [%3d]  term: [%3d]  timeout: [%4d] ms\n", rf.role, rf.me, rf.currentTerm, timeout)

		select {
		case args := <- rf.appendCh:
			rf.mu.Lock()
			DPrintf(rf.log, "warn", "Follower:[%3d] term:[%3d] receive Leader:[%3d] message!\n", rf.me, rf.currentTerm, args.LeaderId)
			rf.mu.Unlock()
		case args := <- rf.voteCh:
			rf.mu.Lock()
			DPrintf(rf.log, "info", "Follower:[%3d] term:[%3d] vote to candidate:[%3d] term[%3d]\n", rf.me, rf.currentTerm, args.CandidateId, args.Term)
			rf.mu.Unlock()
		case <- time.After(time.Duration(timeout) * time.Millisecond):
			rf.mu.Lock()
			DPrintf(rf.log, "info", "Follower:[%3d] term:[%3d] timeout convert to candidate!\n", rf.me, rf.currentTerm)
			rf.role = Candidate
			rf.mu.Unlock()
			return
		}
	}

}