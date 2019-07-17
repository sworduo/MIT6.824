package shardkv


// import "shardmaster"
import (
	"bytes"
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
	request = "request" //clerk request
	newConfig = "newConfig"
	newShard = "newShard"
)

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Type 	string //request or newConfig or newShard
	Shard 	int

	Operation 	string //Put or Append or Get
	Key 	string
	Value 	string
	Clerk 	int64
	CmdIndex 	int

	CfgNum int

	ShardDB 	map[string]string
	ClerkLog 	map[int64]int

	ShardSend 	map[int]struct{} //本配置中，已经发送的shard

}

//type NewShards struct{
//	//接收到新的shard
//	Shard 	int
//	ShardDB 	map[string]string
//	ClerkLog 	map[int64]int
//}
//
//type NewConfig struct{
//	//判断是否执行了新配置，若是,则更新shardSendOrNot
//	CfgNum int
//}

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

	//shardSendOrNot 	bool //shard是否已经发送给新集群->通过判断shardToSend是否为即可
	shardToSend 	map[int]struct{}//配置更新后需要发送的shard
	newAddShards map[int]struct{} //新配置中应该接到的新的shard，去重
	cfg 	shardmaster.Config
	leader 	bool // 判断本server是不是leader

	persister *raft.Persister
}

func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	wrongLeader, wrongGroup := kv.executeOp(Op{request,
												args.Shard,
												getOp,
												args.Key,
												"",
												args.Clerk,
												args.CmdIndex,
												0,
												make(map[string]string),
												make(map[int64]int),
												make(map[int]struct{})})
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
	wrongLeader, wrongGroup := kv.executeOp(Op{request,
												args.Shard,
												op,
												args.Key,
												args.Value,
												args.Clerk,
												args.CmdIndex,
												0,
												make(map[string]string),
												make(map[int64]int),
												make(map[int]struct{})})
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

	kv.cfg = shardmaster.Config{0, [10]int{}, make(map[int][]string)}
	kv.newAddShards = make(map[int]struct{})
	kv.shardToSend = make(map[int]struct{})
	//kv.shardSendOrNot = false
	kv.persister = persister

	raft.ShardInfo.Printf("gid:%2d Me:%2d -> Create a new server!\n", kv.gid, kv.me)

	kv.loadSnapshot()
	//加载数据库和client历史后
	//检查配置信息，更新cfgIndex并且发送shard给其他可能需要的集群。
	go kv.checkCfg()
	go kv.run()


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
		op := msg.Command.(Op)
		msgToChan := cmdOk
		switch op.Type{
		case newConfig:
			if op.CfgNum > kv.cfg.Num{
				//本server还没更新配置，更新之
				kv.getCfg()
			}

			//更新配置后，发送了一部分（可能全部）shard给其他集群
			//将这些集群移除待发送列表
			for shard, _ := range kv.shardToSend{
				if _, ok := op.ShardSend[shard]; ok{
					//清空该shard,节省快照大小
					delete(kv.skvDB, shard)
					delete(kv.sClerkLog, shard)
					//在待发送列表移除已经发送的shard
					delete(kv.shardToSend, shard)
				}
			}

		case newShard:
			if op.CfgNum > kv.cfg.Num{
				//本server还没更新配置，更新之
				kv.getCfg()
			}
			delete(kv.newAddShards, op.Shard)
			kv.skvDB[op.Shard] = make(map[string]string)
			kv.sClerkLog[op.Shard] = make(map[int64]int)
			kv.copyDB(op.ShardDB, kv.skvDB[op.Shard])
			kv.copyCL(op.ClerkLog, kv.sClerkLog[op.Shard])
		case request:

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
		}

		if ch, ok := kv.msgCh[index]; ok{
			ch <- msgToChan
		}else if kv.leader{
			raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| command{%v} no channel index:%2d!\n", kv.gid, kv.cfg.Num, kv.leader, op, index)
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

func (kv *ShardKV) executeOp(op Op)(wrongLeader, wrongGroup bool){
	//执行命令逻辑
	index := 0
	isleader := false
	isOp := op.Type == request
	wrongLeader = true
	wrongGroup = true

	if isOp{
		kv.mu.Lock()
		//判断指令需要的shard加载完成没有
		_, ok := kv.newAddShards[op.Shard]
		if !kv.haveShard(op.Shard) || ok{
			raft.ShardInfo.Printf("GID:%2d cfg:%2d | Do not responsible for this shard %2d\n", kv.gid, kv.cfg.Num, op.Shard)
			raft.ShardInfo.Printf("have:{%v} need to add:{%v}\n", kv.shards, kv.newAddShards)
			raft.ShardInfo.Printf("db:%v\n", kv.skvDB)
			raft.ShardInfo.Printf("cfg:%d ==> %v\n\n", kv.cfg.Num, kv.cfg.Shards)
			kv.mu.Unlock()
			return
		}
		if ind, ok := kv.sClerkLog[op.Shard][op.Clerk]; ok && ind >= op.CmdIndex && kv.leader{
			//指令已经执行过
			//raft.ShardInfo.Printf("GID:%2d me:%2d | Command{%v} have done before\n", kv.gid, kv.me, cmd)
			wrongLeader, wrongGroup = false, false
			kv.mu.Unlock()
			return
		}
		kv.mu.Unlock()
	}
	wrongGroup = false
	//switch op.(type){
	//case NewShards:
	//	index, _, isleader = kv.rf.Start(op.(NewShards))
	//case NewConfig:
	//	index, _, isleader = kv.rf.Start(op.(NewConfig))
	//case Op:
	//	index, _, isleader = kv.rf.Start(op.(Op))
	//	isOp = true
	//}
	index, _, isleader = kv.rf.Start(op)

	kv.mu.Lock()
	kv.leader = isleader

	if !isleader{
		kv.mu.Unlock()
		return
	}
	wrongLeader = false

	ch := make(chan bool)
	kv.msgCh[index] = ch
	kv.mu.Unlock()
	raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| command{%v} begin! index:%2d\n", kv.gid, kv.cfg.Num, kv.leader, op, index)

	select {
	case <- time.After(time.Duration(300) * time.Millisecond):
		wrongLeader = true
		raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| command{%v} index:%2d timeout!\n", kv.gid, kv.cfg.Num, kv.leader, op, index)
	case res := <- ch:
		if res == noLongerHandleThis{
			//不再负责这个shard
			raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| exclude command{%v} index:%2d!\n", kv.gid, kv.cfg.Num, kv.leader, op, index)
			wrongGroup = true
		}else{
			raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| command{%v} index:%2d Done!\n", kv.gid, kv.cfg.Num,kv.leader, op, index)
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
		raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v me:%2d| Update config to %d\n", kv.gid, kv.cfg.Num, kv.leader, kv.me, cfg.Num)
		raft.ShardInfo.Printf("New shard assign:%v\n", cfg.Shards)

		newShards := make(map[int]struct{})

		for shard, gid := range cfg.Shards{
			if gid == kv.gid{
				newShards[shard] = struct{}{}
				if !kv.haveShard(shard){
					//之前没收到的，本轮可能会收到
					//所以不重置newAddShards，而是会直接添加
					kv.newAddShards[shard] = struct{}{}
				}else{
					//删除不存在的元素不会报错
					delete(kv.shards, shard)
				}
			}
		}

		//kv.shardSendOrNot = false
		//没有删除的shard代表本集群不再负责该shard
		for staleShard, _ := range kv.shards{
			//如果上一轮的shard没发送完，继续发送
			//所以shardToSend不需要清空
			//比如配置10,本集群应该将数据D发送给集群B
			//但是集群B在配置10一直集体当即无法发送
			//更新到配置11，数据D分配给集群C
			//此时，本集群直接将数据发送给C，不发给B
			if _, ok := kv.newAddShards[staleShard]; ok{
				//如果上一个配置中，一直没接到某个shard的数据
				//那么新配置中，不会发送这个shard的数据（因为没有）
				//负责这个shard（在kv.shards）中
				//却没收到这个shard（在kv.newAddShards）中
				//更新后却要发送给其他集群

				//按照实现，会由上一个shard集群直接发给下一个集群
				//所以不再等待这个shard
				delete(kv.newAddShards, staleShard)
			}else{
				//拥有这个shard，加入待发送列表
				kv.shardToSend[staleShard] = struct{}{}
			}
		}
		kv.shards = newShards
		if kv.cfg.Num == 0 && kv.persister.SnapshotSize() == 0{
			//第一次加载配置，并且快照为0,直接初始化.
			kv.newAddShards = make(map[int]struct{})
			//第一次配置不会接收其他shard，因此不会同步newShard日志，因此不会创建相应的skvdDB和sClerkLog
			for shard, _ := range kv.shards{
				kv.skvDB[shard] = make(map[string]string)
				kv.sClerkLog[shard] = make(map[int64]int)
			}
		}
		if kv.leader == true {
			kv.sendShard()
		}
		kv.cfg = cfg
	}
}

func (kv *ShardKV) checkCfg(){
	//周期性检查配置信息
	for{
		isleader := kv.checkLeader()
		kv.mu.Lock()
		//检查自己是否时Leader
		//由follower转为leader后，检查待发送列表
		kv.leader = isleader
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
	GID int
}

type MoveShardReply struct{
	WrongLeader 	bool
}

func (kv *ShardKV) checkLeader()bool{
	//判断自己是不是leader
	_, isleader := kv.rf.GetState()
	return isleader
}

func (kv *ShardKV) MoveShard(args *MoveShardArgs, reply *MoveShardReply){
	//接收前判断自己是否是leader
	isleader := kv.checkLeader()
	//新集群接收shard逻辑
	kv.mu.Lock()
	reply.WrongLeader = true

	kv.leader = isleader
	if !kv.leader || args.CfgNum < kv.cfg.Num{
		kv.mu.Unlock()
		return
	}

	//if !kv.haveShard(args.Shard) || args.CfgNum < kv.cfg.Num{
	//	raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v| cfg{arg:%2d->kv:%2d}\n", kv.gid, kv.me, kv.leader, args.CfgNum, kv.cfg.Num)
	//	raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v| shard:%2d  {%v}\n", kv.gid, kv.me, kv.leader, args.Shard, kv.shards)
	//
	//	//过期的数据迁移，丑拒
	//	kv.mu.Unlock()
	//	return
	//}
	if args.CfgNum > kv.cfg.Num{
		//更新
		kv.getCfg()
	}
	//raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| get shard %2d from gid:%2d\n", kv.gid, kv.cfg.Num, kv.leader, args.Shard, args.GID)
	//raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| %v\n", kv.gid, kv.cfg.Num, kv.leader, kv.newAddShards)

	if _, ok := kv.newAddShards[args.Shard]; ok{
		//接收到shard
		//同步
		raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| receive shard %2d from gid:%2d\n", kv.gid, kv.cfg.Num, kv.leader, args.Shard, args.GID)
		op := Op{newShard,
				args.Shard,
				"",
				"",
				"",
				-1,
				-1,
				kv.cfg.Num,
				args.ShardDB,
				args.ClerkLog,
				make(map[int]struct{})}
		kv.mu.Unlock()
		wrongLeader, wrongGroup := kv.executeOp(op)
		kv.mu.Lock()
		if !wrongLeader && !wrongGroup{
			//指令执行成功
			//执行成功，删掉待接收shard
			//该shard已经在执行指令时删除
			//delete(kv.newAddShards, args.Shard)
			reply.WrongLeader = false
			raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| add new shard %d done! \n", kv.gid, kv.cfg.Num, kv.leader, args.Shard)
		}
	}
	kv.mu.Unlock()
	return

}

func (kv *ShardKV) sendShard(){
	//调用函数默认有锁
	//旧集群发送shard逻辑
	if len(kv.shardToSend) == 0{
		//本轮已经发送过了
		//没有需要发送的shard
		return
	}

	raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| cfgnum:%2d transfer %d shard\n", kv.gid, kv.cfg.Num, kv.leader, kv.cfg.Num, len(kv.shardToSend))
	//发送shard
	shardsend := make(map[int]struct{})

	for shard, _ := range kv.shardToSend{
		gid := kv.cfg.Shards[shard]
		kvDB := make(map[string]string)
		ckLog := make(map[int64]int)
		kv.copyDB(kv.skvDB[shard], kvDB)
		kv.copyCL(kv.sClerkLog[shard], ckLog)

		args := MoveShardArgs{kv.cfg.Num, shard,kvDB,ckLog, kv.gid}

		if servers, ok := kv.cfg.Groups[gid]; ok {
			// try each server for the shard.
			raft.ShardInfo.Printf("GID:%2d cfg:%2d me:%2d leader:%6v| transfer shard %2d to gid:%2d\n", kv.gid, kv.cfg.Num, kv.me, kv.leader, shard, gid)
			for si := 0; si < len(servers); si++ {
				srv := kv.make_end(servers[si])
				var reply MoveShardReply
				kv.mu.Unlock()
				ok := srv.Call("ShardKV.MoveShard", &args, &reply)
				kv.mu.Lock()
				if ok && reply.WrongLeader == false {
					//发送完一个，在记录中删除
					//交付日志时会删除
					// delete(kv.shardToSend, shard)
					//清除该shard数据
					//delete(kv.skvDB, shard)
					//delete(kv.sClerkLog, shard)

					raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| OK! transfer shard %2d to gid:%2d\n", kv.gid, kv.cfg.Num, kv.leader, shard, gid)

					//记录本轮发送完的shard
					shardsend[shard] = struct{}{}
					break
				}
			}
		}
	}

	if len(shardsend) > 0{
		//本轮有没有发送shard给其他集群
		//有时候目标集群一直无法发送
		//如果没有发送shard就不同步日志

		op := Op{newConfig,
				-1,
				"",
				"",
				"",
				-1,
				-1,
				kv.cfg.Num,
				make(map[string]string),
				make(map[int64]int),
				shardsend}

		//必须用goroutine
		//否则run->sendShard->executeOp->start等待rf.mu->阻塞executeOp->阻塞sendShard->阻塞run
		//run阻塞->raft无法交付日志->ApplyMsg 一直占用rf.mu->start无法获取rf.mu
		//死锁
		go kv.executeOp(op)
	}

	//调用函数默认有锁

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
	var nas map[int]struct{}
	var sts map[int]struct{}
	var sd map[int]struct{}

	if dec.Decode(&db) != nil || dec.Decode(&cl) != nil || dec.Decode(&nas) != nil || dec.Decode(&sts) != nil || dec.Decode(&sd) != nil{
		raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v| KV Failed to recover by snapshot!\n", kv.gid, kv.me, kv.leader)
	}else{
		kv.skvDB = db
		kv.sClerkLog = cl
		kv.newAddShards = nas
		kv.shardToSend = sts
		kv.shards = sd
		raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v| KV recover from snapshot successful! \n", kv.gid, kv.me, kv.leader)
	}
}

func (kv *ShardKV) checkState(index int, term int){
	//判断raft日志长度
	if kv.maxraftstate == -1{
		return
	}
	//日志长度接近时，启动快照
	//因为rf连续提交日志后才会释放rf.mu，所以需要提前发出快照调用
	portion := 3 / 4
	//一个log的字节长度不是1，而可能是几十字节，所以可能仅仅几十个命令的raftStateSize就超过1000了。
	//几个log的字节大小可能就几百字节了，所以快照要趁早
	if kv.persister.RaftStateSize() < kv.maxraftstate * portion{
		return
	}

	raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v cfg:%2d| Begin snapshot! \n", kv.gid, kv.me, kv.leader, kv.cfg.Num)
	rawSnapshot := kv.encodeSnapshot()
	go func() {kv.rf.TakeSnapshot(rawSnapshot, index, term)}()
}

func (kv *ShardKV) encodeSnapshot() []byte {
	//调用者默认拥有kv.mu
	w := new(bytes.Buffer)
	enc := labgob.NewEncoder(w)
	enc.Encode(kv.skvDB)
	enc.Encode(kv.sClerkLog)
	enc.Encode(kv.newAddShards)
	enc.Encode(kv.shardToSend)
	enc.Encode(kv.shards)
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