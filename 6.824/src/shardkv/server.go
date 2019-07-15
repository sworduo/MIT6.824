package shardkv


// import "shardmaster"
import (
	"bytes"
	"kvraft"
	"labrpc"
	"shardmaster"
	"time"
)
import "raft"
import "sync"
import "labgob"

const (
	putOp = "put"
	appendOp = "append"
	getOp = "get"
	//日志同步成功，判断本集群是否还负责此shard
	cmdOk = true
	noLongerHandleThis = false

)

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Operation 	string //Put or Append or Get
	Key 	string
	Value 	string
	Shard 	int
	Clerk 	int64
	CmdIndex 	int
}

type NewShards struct{
	//接收到新的shard
	Shard 	int
	ShardDB 	map[string]string
	ClerkLog 	map[int64]int
}

type NewConfig struct{
	//判断是否执行了新配置，若是,则更新shardSendOrNot
	CfgNum int
}

type ShardKV struct {
	mu           sync.Mutex
	me           int
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	make_end     func(string) *labrpc.ClientEnd
	gid          int
	masters      []*labrpc.ClientEnd
	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	msgCh 	map[int]chan bool
	sClerkLog 	map[int]map[int64]int //shard -> clerkLog
	skvDB 	map[int]map[string]string //shard -> kvDB
	sm *shardmaster.Clerk
	shards 	map[int]struct{} //集群所管理的shard

	shardSendOrNot 	bool //shard是否已经发送给新集群
	shardToSend 	map[int]struct{}//配置更新后需要发送的shard
	newAddShards map[int]bool //新配置中所应该接到的新的shard，去重  shrad -> receive or not
	cfg 	shardmaster.Config
	leader 	bool // 判断本server是不是leader

	persister *raft.Persister
}

func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	wrongLeader, wrongGroup := kv.executeOp(Op{getOp,
												args.Key,
												"",
												args.Shard,
												args.Clerk,
												args.CmdIndex})
	reply.WrongLeader = wrongLeader
	if wrongGroup{
		reply.Err = ErrWrongGroup
		return
	}
	if wrongLeader{
		return
	}
	kv.mu.Lock()
	if val, ok := kv.skvDB[args.Shard][args.Key]; ok{
		reply.Value = val
		reply.Err = OK
	}else{
		reply.Value = ""
		reply.Err = ErrNoKey
	}
	kv.mu.Unlock()
	return
}

func (kv *ShardKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	var op string
	if args.Op == "Put"{
		op = putOp
	}else{
		op = appendOp
	}
	wrongLeader, wrongGroup := kv.executeOp(Op{op,
												args.Key,
												args.Value,
												args.Shard,
												args.Clerk,
												args.CmdIndex})
	reply.WrongLeader = wrongLeader
	reply.Err = OK
	if wrongGroup{
		reply.Err = ErrWrongGroup
	}
	return
}

//
// the tester calls Kill() when a ShardKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *ShardKV) Kill() {
	kv.rf.Kill()
	// Your code here, if desired.
	raft.ShardInfo.Printf("gid:%2d Me:%2d  <- I am died!\n", kv.gid, kv.me)
}


//
// servers[] contains the ports of the servers in this group.
//
// me is the index of the current server in servers[].
//
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
//
// the k/v server should snapshot when Raft's saved state exceeds
// maxraftstate bytes, in order to allow Raft to garbage-collect its
// log. if maxraftstate is -1, you don't need to snapshot.
//
// gid is this group's GID, for interacting with the shardmaster.
//
// pass masters[] to shardmaster.MakeClerk() so you can send
// RPCs to the shardmaster.
//
// make_end(servername) turns a server name from a
// Config.Groups[gid][i] into a labrpc.ClientEnd on which you can
// send RPCs. You'll need this to send RPCs to other groups.
//
// look at client.go for examples of how to use masters[]
// and make_end() to send RPCs to the group owning a specific shard.
//
// StartServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int, gid int, masters []*labrpc.ClientEnd, make_end func(string) *labrpc.ClientEnd) *ShardKV {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Op{})

	kv := new(ShardKV)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.make_end = make_end
	kv.gid = gid
	kv.masters = masters

	// Your initialization code here.

	// Use something like this to talk to the shardmaster:
	// kv.mck = shardmaster.MakeClerk(kv.masters)

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)
	kv.sm = shardmaster.MakeClerk(kv.masters)
	kv.msgCh = make(map[int]chan bool)
	kv.sClerkLog = make(map[int]map[int64]int)
	kv.skvDB = make(map[int]map[string]string)
	kv.shards = make(map[int]struct{})

	kv.cfg = shardmaster.Config{}
	kv.newAddShards = make(map[int]bool)
	kv.shardToSend = make(map[int]struct{})
	kv.shardSendOrNot = false
	kv.persister = persister

	raft.ShardInfo.Printf("gid:%2d Me:%2d -> Create a new server!\n", kv.gid, kv.me)

	kv.loadSnapshot()
	//加载数据库和client历史后
	//检查配置信息，更新cfgIndex并且发送shard给其他可能需要的集群。
	go kv.run()
	go kv.checkCfg()

	return kv
}

func (kv *ShardKV) run(){
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

		switch msg.Command.(type) {
		case NewConfig:
			if msg.Command.(NewConfig).CfgNum > kv.cfg.Num{
				//本server还没更新配置，更新之
				kv.getCfg()
			}
			if msg.Command.(NewConfig).CfgNum == kv.cfg.Num{
				//更新配置后，旧shard已经发送给其他集群了
				kv.shardSendOrNot = true
				for shard, _ := range kv.shardToSend{
					//清空该shard,节省快照大小
					delete(kv.skvDB, shard)
					delete(kv.sClerkLog, shard)
				}
				//清空需要发送的shard列表
				kv.shardToSend = make(map[int]struct{})
			}
		case NewShards:
			ns := msg.Command.(NewShards)
			kv.copyDB(ns.ShardDB, kv.skvDB[ns.Shard])
			kv.copyCL(ns.ClerkLog, kv.sClerkLog[ns.Shard])
		case Op:
			op := msg.Command.(Op)
			msgToChan := cmdOk

			if ind, ok := kv.sClerkLog[op.Shard][op.Clerk]; ok && ind >= op.CmdIndex {
				//如果clerk存在，并且该指令已经执行，啥也不做
			}else if !kv.haveShard(op.Shard){
				//本集群不再负责这个shard
				msgToChan = noLongerHandleThis
			}else{
				//执行指令
				kv.sClerkLog[op.Shard][op.Clerk] = op.CmdIndex
				switch op.Operation {
				case putOp:
					kv.skvDB[op.Shard][op.Key] = op.Value
				case appendOp:
					if _, ok := kv.skvDB[op.Shard][op.Key]; ok {
						kv.skvDB[op.Shard][op.Key] = kv.skvDB[op.Shard][op.Key] + op.Value
					} else {
						kv.skvDB[op.Shard][op.Key] = op.Value
					}
				case getOp:
				}
			}
			//只有leader才有管道，所以只有leader才会通知
			//旧laeder通知时，term不一样，rpc调用失败
			//新leader没有管道，但是已执行指令，下一次RPC到来时直接返回

			//其实旧leader也可以返回执行成功，因为这代表该指令已经成功执行了，没必要再发送一次

			if ch, ok := kv.msgCh[index]; ok{
				ch <- msgToChan
			}
		}
		if kv.leader{
			//判断是否有shard没发送成功
			kv.sendShard()
		}
		//要放在指令执行之后才检查状态
		//因为index是所保存快照最后一条执行的指令
		//如果放在index指令执行前检测，那么保存的快照将不包含index这条指令
		kv.checkState(index, term)
		kv.mu.Unlock()
	}
}

//===============================================================================
//一些辅助函数
//===============================================================================
func (kv *ShardKV)haveShard(shard int) bool{
	//调用函数默认有锁
	//判断集群是否有这个shard
	_, ok := kv.shards[shard]
	return ok
}

func (kv *ShardKV) copyDB(src map[string]string, des map[string]string){
	for k,v := range src{
		des[k] = v
	}
}

func (kv *ShardKV) copyCL(src map[int64]int, des map[int64]int){
	for k,v := range src{
		des[k] = v
	}
}

func (kv *ShardKV) closeCh(index int){
	kv.mu.Lock()
	defer kv.mu.Unlock()
	close(kv.msgCh[index])
	delete(kv.msgCh, index)
}

func (kv *ShardKV) executeOp(op interface{})(wrongLeader, wrongGroup bool){
	//执行命令逻辑
	index := 0
	isleader := false
	isOp := false
	wrongLeader = true
	wrongGroup = true
	switch op.(type){
	case NewShards:
		index, _, isleader = kv.rf.Start(op.(NewShards))
	case NewConfig:
		index, _, isleader = kv.rf.Start(op.(NewConfig))
	case Op:
		index, _, isleader = kv.rf.Start(op.(Op))
		isOp = true
	}

	kv.mu.Lock()
	kv.leader = isleader


	if isOp && !kv.haveShard(op.(Op).Shard){
		kv.mu.Unlock()
		return
	}
	wrongGroup = false
	if !isleader{
		kv.mu.Unlock()
		return
	}
	wrongLeader = false
	if isOp{
		cmd := op.(Op)
		raft.ShardInfo.Printf("GID:%2d me:%2d | Receive %s from {%d->%d}\n", kv.gid, kv.me, cmd.Operation, cmd.Clerk, cmd.CmdIndex)
		if ind, ok := kv.sClerkLog[cmd.Shard][cmd.Clerk]; ok && ind >= cmd.CmdIndex{
			//指令已经执行过
			kv.mu.Unlock()
			return
		}
	}

	ch := make(chan bool)
	kv.msgCh[index] = ch
	kv.mu.Unlock()


	select {
	case <- time.After(raftkv.WaitPeriod):
	case res := <- ch:
		if res == noLongerHandleThis{
			//不再负责这个shard
			wrongGroup = true
		}else{
			raft.ShardInfo.Printf("GID:%2d me:%2d | command{%T} Done!\n", kv.gid, kv.me, op)
		}
	}
	//raft.ShardInfo.Printf("GID:%2d me:%2d | command{%T} Done!\n", kv.gid, kv.me, op)
	go kv.closeCh(index)
	return
}

//===============================================================================
//获取配置信息
//===============================================================================
func (kv *ShardKV) getCfg(){
	//调用函数默认有锁
	cfg := kv.sm.Query(-1)
	if cfg.Num > kv.cfg.Num{
		//配置更新
		kv.cfg = cfg
		raft.ShardInfo.Printf("GID:%2d me:%2d | Update config to %d\n", kv.gid, kv.me, cfg.Num)
		newShards := make(map[int]struct{})
		newAddShards := make(map[int]bool)
		for shard, gid := range cfg.Shards{
			if gid == kv.gid{
				newShards[shard] = struct{}{}
				if !kv.haveShard(shard){
					newAddShards[shard] = false
					kv.skvDB[shard] = make(map[string]string)
					kv.sClerkLog[shard] = make(map[int64]int)
				}else{
					delete(kv.shards, shard)
				}
			}
		}

		kv.shardSendOrNot = false
		//没有删除的shard代表本集群不再负责该shard
		for staleShard, _ := range kv.shards{
			//如果上一轮的shard没发送完，继续发送
			//所以shardToSend不需要清空
			kv.shardToSend[staleShard] = struct{}{}
		}
		kv.shards = newShards
		kv.newAddShards = newAddShards
		if kv.leader == true {
			kv.sendShard()
		}
	}
}

func (kv *ShardKV) checkCfg(){
	//周期性检查配置信息
	for{
		kv.mu.Lock()
		kv.getCfg()
		kv.mu.Unlock()
		time.Sleep(time.Duration(100) * time.Millisecond)
	}
}

//===============================================================================
//配置更新后，迁移/接收shard
//===============================================================================
type MoveShardArgs struct{
	CfgNum int
	Shard 	int
	ShardDB map[string]string
	ClerkLog map[int64]int
}

type MoveShardReply struct{
	WrongLeader 	bool
}

func (kv *ShardKV) MoveShard(args *MoveShardArgs, reply *MoveShardReply){
	//新集群接收shard逻辑
	kv.mu.Lock()

	if !kv.leader{
		reply.WrongLeader = true
		kv.mu.Unlock()
		return
	}

	reply.WrongLeader = false
	if !kv.haveShard(args.Shard) || args.CfgNum < kv.cfg.Num{
		//过期的数据迁移，丑拒
		kv.mu.Unlock()
		return
	}
	if args.CfgNum > kv.cfg.Num{
		//更新
		kv.getCfg()
	}
	if _, ok := kv.sClerkLog[args.Shard]; !ok{
		//接收到shard
		//同步
		raft.ShardInfo.Printf("GID:%2d me:%2d | receive shard %2d\n", kv.gid, kv.me, args.Shard)
		kv.mu.Unlock()
		wrongLeader, wrongGroup := kv.executeOp(NewShards{args.Shard,args.ShardDB,args.ClerkLog})
		kv.mu.Lock()
		if !wrongLeader || !wrongGroup{
			//指令执行失败
			reply.WrongLeader = true
		}
	}
	kv.mu.Unlock()
	return

}

func (kv *ShardKV) sendShard(){
	//调用函数默认有锁
	//旧集群发送shard逻辑
	if kv.shardSendOrNot{
		//本轮已经发送过了
		return
	}

	//发送shard
	for shard, _ := range kv.shardToSend{
		gid := kv.cfg.Shards[shard]
		kvDB := make(map[string]string)
		ckLog := make(map[int64]int)
		kv.copyDB(kv.skvDB[shard], kvDB)
		kv.copyCL(kv.sClerkLog[shard], ckLog)

		args := MoveShardArgs{kv.cfg.Num, shard,kvDB,ckLog}
		if servers, ok := kv.cfg.Groups[gid]; ok {
			// try each server for the shard.
			for si := 0; si < len(servers); si++ {
				srv := kv.make_end(servers[si])
				var reply MoveShardReply
				raft.ShardInfo.Printf("GID:%2d me:%2d | transfer shard %2d to gid:%2d\n", kv.gid, kv.me, shard, gid)
				kv.mu.Unlock()
				ok := srv.Call("ShardKV.MoveShard", &args, &reply)
				kv.mu.Lock()
				if ok && reply.WrongLeader == false {
					//发送完一个，在记录中删除
					delete(kv.shardToSend, shard)
					//清除该shard数据
					delete(kv.skvDB, shard)
					delete(kv.sClerkLog, shard)
					break
				}
			}
		}
	}
	if len(kv.shardToSend) != 0{
		//不等于0,代表有的shard发送失败
		//leader会周期检查是否有shard没发送成功

		//调用函数默认有锁
		return
	}
	kv.shardSendOrNot = true
	cfgNum := kv.cfg.Num

	kv.mu.Unlock()
	//同步日志，表示新一轮配置中本集群已经将shard发送完成
	kv.executeOp(NewConfig{cfgNum})
	//调用函数默认有锁
	kv.mu.Lock()

}


//===============================================================================
//快照编码
//===============================================================================
func (kv *ShardKV) decodedSnapshot(data []byte){
	//调用此函数时，，默认调用者持有kv.mu
	r := bytes.NewBuffer(data)
	dec := labgob.NewDecoder(r)

	var db	map[int]map[string]string
	var cl  map[int]map[int64]int

	if dec.Decode(&db) != nil || dec.Decode(&cl) != nil{
		raft.InfoKV.Printf("KVServer:%2d | KV Failed to recover by snapshot!\n", kv.me)
	}else{
		kv.skvDB = db
		kv.sClerkLog = cl
		raft.InfoKV.Printf("KVServer:%2d | KV recover frome snapshot successful! \n", kv.me)
	}
}

func (kv *ShardKV) checkState(index int, term int){
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

	rawSnapshot := kv.encodeSnapshot()
	go func() {kv.rf.TakeSnapshot(rawSnapshot, index, term)}()
}

func (kv *ShardKV) encodeSnapshot() []byte {
	//调用者默认拥有kv.mu
	w := new(bytes.Buffer)
	enc := labgob.NewEncoder(w)
	enc.Encode(kv.skvDB)
	enc.Encode(kv.sClerkLog)
	data := w.Bytes()
	return data
}

func (kv *ShardKV) loadSnapshot(){
	data := kv.persister.ReadSnapshot()
	if data == nil || len(data) == 0{
		return
	}
	kv.decodedSnapshot(data)
}