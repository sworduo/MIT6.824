package raftkv

import (
	"bytes"
	"labgob"
	"labrpc"
	"log"
	"raft"
	"sync"
	"time"
)

const (
	Debug = 0
	WaitPeriod = time.Duration(1000) * time.Millisecond //请求响应的等待超时时间
)

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}


type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Method 	string //Put or Append or Get
	Key 	string
	Value 	string
	Clerk 	int64 //哪个clerk发出的
	Index 	int // 这个clerk的第几条命令
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	clerkLog map[int64]int 	//记录每一个clerk已执行的命令编号
	kvDB 	map[string]string //保存key value
	msgCh 	map[int]chan int //消息通知的管道
	persister 	*raft.Persister

}


func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	op := Op{"Get", args.Key, "", args.ClerkID, args.CmdIndex}
	//raft.InfoKV.Printf("KVServer:%2d | receive RPC! Clerk:[%20v] index:[%4d]\n", kv.me, op.Clerk, op.Index)
	reply.Err = ErrNoKey
	reply.WrongLeader = true

	//raft.InfoKV.Printf("KVServer:%2d | Begin Method:[%s] clerk:[%20v] index:[%4d]\n", kv.me, op.Method, op.Clerk, op.Index)
	index, term, isLeader := kv.rf.Start(op)
	if !isLeader{
		//raft.InfoKV.Printf("KVServer:%2d | Sry, I am not leader\n", kv.me)
		return
	}

	kv.mu.Lock()
	if ind, ok := kv.clerkLog[args.ClerkID]; ok && ind >= args.CmdIndex{
		//该指令已经执行
		//raft.InfoKV.Printf("KVServer:%2d | Cmd has been finished: Method:[%s] clerk:[%v] index:[%4d]\n", kv.me, op.Method, op.Clerk, op.Index)
		reply.Value = kv.kvDB[args.Key]
		kv.mu.Unlock()
		reply.WrongLeader = false
		reply.Err = OK
		return
	}

	raft.InfoKV.Printf(("KVServer:%2d | leader msgIndex:%4d\n"), kv.me, index)
	//新建ch再放入msgCh的好处是，下面select直接用ch即可
	//而不是直接等待kv.msgCh[index]
	ch := make(chan int)
	kv.msgCh[index] = ch
	kv.mu.Unlock()

	select{
	case <- time.After(WaitPeriod):
		//超时还没有提交，多半是废了
		raft.InfoKV.Printf("KVServer:%2d |Get {index:%4d term:%4d} failed! Timeout!\n", kv.me, index, term)
	case msgTerm := <- ch:
		if msgTerm == term {
			//命令执行
			kv.mu.Lock()
			raft.InfoKV.Printf("KVServer:%2d | Get {index:%4d term:%4d} OK!\n", kv.me, index, term)
			if val, ok := kv.kvDB[args.Key]; ok{
				reply.Value = val
				reply.Err = OK
			}
			kv.mu.Unlock()
			reply.WrongLeader = false
		}else{
			raft.InfoKV.Printf("KVServer:%2d |Get {index:%4d term:%4d} failed! Not leader any more!\n", kv.me, index, term)
		}
	}

	go func() {kv.closeCh(index)}()
	return
}

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	op := Op{args.Op, args.Key, args.Value, args.ClerkID, args.CmdIndex}
	//raft.InfoKV.Printf("KVServer:%2d | receive RPC! Clerk:[%20v] index:[%4d]\n", kv.me, op.Clerk, op.Index)
	reply.Err = OK
	kv.mu.Lock()

	//follower收到已经执行的put append请求，直接返回
	if ind, ok := kv.clerkLog[args.ClerkID]; ok && ind >= args.CmdIndex{
		//该指令已经执行
		kv.mu.Unlock()
		//raft.InfoKV.Printf("KVServer:%2d | Cmd has been finished: Method:[%s] clerk:[%v] index:[%4d]\n", kv.me, op.Method, op.Clerk, op.Index)
		reply.WrongLeader = false
		return
	}
	kv.mu.Unlock()


	//raft.InfoKV.Printf("KVServer:%2d | Begin Method:[%s] clerk:[%20v] index:[%4d]\n", kv.me, op.Method, op.Clerk, op.Index)
	index, term, isLeader := kv.rf.Start(op)
	if !isLeader{
		//raft.InfoKV.Printf("KVServer:%2d | Sry, I am not leader\n", kv.me)
		reply.WrongLeader = true
		return
	}

	//start之后才做这个有问题
	//start，表示命令已经提交给raft实例
	//如果下面apply那里一直占用kv.mu.lock()
	//这里一直得不到kv.mu.Lock()，显然，当下面通过管道交付时，会发现管道根本就不存在！然后就死锁了。

	//记得改！！必须先申请管道，然后再start，如果start失败再删除管道！！
	//申请管道需要start的index，不能先申请管道
	//但是把start和申请管道放在同一个锁里，就能保证start和申请管道原子操作
	//start是调用本机的其他程序， 不是RPC调用，放在一个锁里应该没关系吧
	kv.mu.Lock()
	raft.InfoKV.Printf(("KVServer:%2d | leader msgIndex:%4d\n"), kv.me, index)
	ch := make(chan int)
	kv.msgCh[index] = ch
	kv.mu.Unlock()

	reply.WrongLeader = true
	select{
	case <- time.After(WaitPeriod):
		//超时还没有提交，多半是废了
		raft.InfoKV.Printf("KVServer:%2d | Put {index:%4d term:%4d} Failed, timeout!\n", kv.me, index, term)
	case msgTerm := <- ch:
		if msgTerm == term {
			//命令执行，或者已经执行过了
			raft.InfoKV.Printf("KVServer:%2d | Put {index:%4d term:%4d} OK!\n", kv.me, index, term)
			reply.WrongLeader = false
		}else{
			raft.InfoKV.Printf("KVServer:%2d | Put {index:%4d term:%4d} Failed, not leader!\n", kv.me, index, term)
		}
	}
	go func() {kv.closeCh(index)}()
	return
}

//
// the tester calls Kill() when a KVServer instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *KVServer) Kill() {
	kv.rf.Kill()
	// Your code here, if desired.
	kv.mu.Lock()
	raft.InfoKV.Printf("KVServer:%2d | KV server is died!\n", kv.me)
	kv.mu.Unlock()
	//底层rf删掉之后，上层的kv server不再交流和更新信息，相当于挂了，所以不用做任何事情
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Op{})

	kv := new(KVServer)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// You may need initialization code here.

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	// You may need initialization code here.

	kv.kvDB = make(map[string]string)
	kv.clerkLog = make(map[int64]int)
	kv.msgCh = make(map[int]chan int)
	kv.persister = persister

	kv.loadSnapshot()

	raft.InfoKV.Printf("KVServer:%2d | Create New KV server!\n", kv.me)

	go kv.receiveNewMsg()


	return kv
}

func (kv *KVServer) receiveNewMsg(){
		for msg := range kv.applyCh {
			kv.mu.Lock()
			//按序执行指令
			index := msg.CommandIndex
			term := msg.CommitTerm
			//role := msg.Role

			if !msg.CommandValid{
				//snapshot
				op := msg.Command.([]byte)
				kv.decodedSnapshot(op)
				kv.mu.Unlock()
				continue
			}

			op := msg.Command.(Op)

			if ind, ok := kv.clerkLog[op.Clerk]; ok && ind >= op.Index {
				//如果clerk存在，并且该指令已经执行，啥也不做

				//会不会出现index顺序为6 8 7的情况？
				//不会，因为只有前一条指令成功执行,clerk才会发送后一条指令
				//只会有重复指令，而不会出现跳跃指令
				//raft.InfoKV.Printf("KVServer:%2d | Cmd has been finished: Method:[%s] clerk:[%v] Cindex:[%4d]\n", kv.me, op.Method, op.Clerk, op.Index)
			}else{
				//执行指令
				kv.clerkLog[op.Clerk] = op.Index
				switch op.Method {
				case "Put":
					kv.kvDB[op.Key] = op.Value
					//if role == raft.Leader {
					//	raft.InfoKV.Printf("KVServer:%2d | Put successful!  clerk:[%v] Cindex:[%4d] Mindex:[%4d]\n", kv.me, op.Clerk, op.Index, index)
					//}
				case "Append":
					if _, ok := kv.kvDB[op.Key]; ok {
						kv.kvDB[op.Key] = kv.kvDB[op.Key] + op.Value
					} else {
						kv.kvDB[op.Key] = op.Value
					}
					//if role == raft.Leader {
					//	raft.InfoKV.Printf("KVServer:%2d | Append successful! clerk:[%v] Cindex:[%4d] Mindex:[%4d]\n", kv.me, op.Clerk, op.Index, index)
					//}
				case "Get":
					//if role == raft.Leader {
					//	raft.InfoKV.Printf("KVServer:%2d | Get successful! clerk:[%v] Cindex:[%4d] Mindex:[%4d]\n", kv.me, op.Clerk, op.Index, index)
					//}
				}
			}
			//只有leader才有管道，所以只有leader才会通知
			//旧laeder通知时，term不一样，rpc调用失败
			//新leader没有管道，但是已执行指令，下一次RPC到来时直接返回

			//其实旧leader也可以返回执行成功，因为这代表该指令已经成功执行了，没必要再发送一次
			if ch, ok := kv.msgCh[index]; ok{
				ch <- term
			}

			//要放在指令执行之后才检查状态
			//因为index是所保存快照最后一条执行的指令
			//如果放在index指令执行前检测，那么保存的快照将不包含index这条指令
			kv.checkState(index, term)
			kv.mu.Unlock()
		}
}

func (kv *KVServer) closeCh(index int){
	kv.mu.Lock()
	defer kv.mu.Unlock()
	close(kv.msgCh[index])
	delete(kv.msgCh, index)
}

func (kv *KVServer) decodedSnapshot(data []byte){
	//调用此函数时，，默认调用者持有kv.mu
	r := bytes.NewBuffer(data)
	dec := labgob.NewDecoder(r)

	var db	map[string]string
	var cl  map[int64]int

	if dec.Decode(&db) != nil || dec.Decode(&cl) != nil{
		raft.InfoKV.Printf("KVServer:%2d | KV Failed to recover by snapshot!\n", kv.me)
	}else{
		kv.kvDB = db
		kv.clerkLog = cl
		raft.InfoKV.Printf("KVServer:%2d | KV recover frome snapshot successful! \n", kv.me)
	}
}

func (kv *KVServer) checkState(index int, term int){
	//判断raft日志长度
	if kv.maxraftstate == -1{
		return
	}

	//日志长度接近时，启动快照
	//因为rf连续提交日志后才会释放rf.mu，所以需要提前发出快照调用
	portion := 2 / 3
	//一个log的字节长度不是1，而可能是几十字节，所以可能仅仅几十个命令的raftStateSize就超过1000了。
	//几个log的字节大小可能就几百字节了，所以快照要趁早
	if kv.persister.RaftStateSize() < kv.maxraftstate * portion{
		return
	}
	//因为takeSnapshot需要rf.mu
	//所以使用goroutine防止rf.mu阻塞

	//下面这个会报错
	//goroutine还没执行，本函数返回，然后取新的命令，执行命令修改kvDB
	//但同时！下面的goroutine执行了，而且执行时不需要拿kv.mu锁
	//因此造成了在encodeSnapshot里读，在主goroutine里写，两种情况同时发生。
	//go func() {kv.rf.TakeSnapshot(kv.encodeSnapshot(), index, term)}()
	rawSnapshot := kv.encodeSnapshot()
	go func() {kv.rf.TakeSnapshot(rawSnapshot, index, term)}()
}

func (kv *KVServer) encodeSnapshot() []byte {
	//调用者默认拥有kv.mu
	w := new(bytes.Buffer)
	enc := labgob.NewEncoder(w)
	enc.Encode(kv.kvDB)
	enc.Encode(kv.clerkLog)
	data := w.Bytes()
	return data
}

func (kv *KVServer) loadSnapshot(){
	data := kv.persister.ReadSnapshot()
	if data == nil || len(data) == 0{
		return
	}
	kv.decodedSnapshot(data)
}