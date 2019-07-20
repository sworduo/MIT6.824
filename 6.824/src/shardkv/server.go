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
	newSend = "newsend"
	Cfg_Get_Time = time.Duration(66) * time.Millisecond
	Send_Shard_Wait = time.Duration(49) * time.Millisecond //发送shard的等待间隔
	RPC_CALL_TIMEOUT = time.Duration(1000) * time.Millisecond //执行指令等待超时时间
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

	NewConfig NewConfig
	NewShards NewShards

}

type NewShards struct{
	//接收到新的shard
	ShardDB 	map[string]string
	ClerkLog 	map[int64]int
	Replace 	bool
}

type NewConfig struct{
	//判断是否执行了新配置，若是,则更新shardSendOrNot
	Cfg 	shardmaster.Config
	//Shards 	map[int]struct{}
	//NewAddShards map[int]struct{}
	//ShardToSend 	map[int]struct{}
	Initial 	bool //是否是  第一次启动且无快照且需要默认初始化
	//ShardCnt int //发送配置时，接受了多少shard
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

	shardToSend 	[]int//配置更新后需要发送的shard
	//newAddShards map[int]struct{} //新配置中应该接到的新的shard，去重
	cfg 	shardmaster.Config
	leader 	bool // 判断本server是不是leader

	persister *raft.Persister

	shardUnderSend map[int]int //发送过程中的shard

	//shardCnt int //计算接受了多少shard
	//shardSendOrNot bool //保证一次只有一个发送shard的goroutine在运行
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
												NewConfig{},
												NewShards{}})
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
												NewConfig{},
												NewShards{}})
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
	raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v ============>prepare to died<===========!\n", kv.gid, kv.me, kv.leader)
	kv.rf.Kill()
	// Your code here, if desired.
	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v <- I am died!\n", kv.gid, kv.me, kv.cfg.Num, kv.leader)
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
	//kv.newAddShards = make(map[int]struct{})
	kv.shardToSend = make([]int, 0)
	//kv.shardSendOrNot = false
	kv.persister = persister
	//kv.shardCnt = 0
	//kv.shardSendOrNot = false
	kv.leader = false

	kv.shardUnderSend = make(map[int]int)

	raft.ShardInfo.Printf("GID:%2d me:%2d -> Create a new server!\n", kv.gid, kv.me)

	kv.loadSnapshot()
	if kv.persister.SnapshotSize() == 0{
		//没有快照，强制从cfg 1开始
		go kv.checkCfg(true)
	}else{
		//有快照，生成发送列表
		kv.parseCfg()
		go kv.checkCfg(false)
	}
	//加载数据库和client历史后
	//检查配置信息，更新cfgIndex并且发送shard给其他可能需要的集群。
	go kv.run()


	return kv
}

func (kv *ShardKV) run(){
	for msg := range kv.applyCh {
		//raft.ShardInfo.Printf("GID:%2d me:%2d | Wait {%v}\n", kv.gid, kv.me, msg)
		kv.mu.Lock()
		//按序执行指令
		index := msg.CommandIndex
		term := msg.CommitTerm
		//role := msg.Role

		if !msg.CommandValid{
			//snapshot
			op := msg.Command.([]byte)
			kv.decodedSnapshot(op)
			//恢复快照后，生成新的shards
			kv.parseCfg()
			kv.mu.Unlock()
			continue
		}
		op := msg.Command.(Op)
		//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|get a lock{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op)

		msgToChan := cmdOk
		switch op.Type{
		case newSend:
			//发送新的shard
			//发送一个，同步一个
			//防止生成发送列表，发送时配置更新，发送给了自己==>这个在发送时排除

			//如果配置10发送shard8,更新到配置中11,shard8又属于本集群
			//此时配置10的发送成功，即便更新到配置中1,此时也应该删除数据
			//因为在配置中0发送成功后，新集群可能接收到新请求
			//本集群拥有的数据在配置11很可能过时
			if !kv.haveShard(op.Shard) && kv.shardInHand(op.Shard) {
				delete(kv.skvDB, op.Shard)
				delete(kv.sClerkLog, op.Shard)
			}
			//delete(kv.shardUnderSend, op.Shard)

		case newConfig:
			//更新配置
			nc := op.NewConfig
			//防止一个cfg更新两次
			if nc.Cfg.Num > kv.cfg.Num{
				//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|execute config\n", kv.gid, kv.me, kv.cfg.Num, kv.leader)
				//收到新的配置
				kv.cfg = nc.Cfg
				kv.parseCfg()
				//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|execute parse done\n", kv.gid, kv.me, kv.cfg.Num, kv.leader)

				if nc.Initial{
					raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|Default initialize\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, kv.shards)
					for shard, _ := range kv.shards{
						kv.skvDB[shard] = make(map[string]string)
						kv.sClerkLog[shard] = make(map[int64]int)
					}
				}
			}
		case newShard:
			if !kv.haveShard(op.Shard){
				msgToChan = noLongerHandleThis
			}else if !kv.shardInHand(op.Shard) || op.NewShards.Replace{
				//考虑在真正add shard前收到了两次相同的moveShard
				//防止执行两次
				ns := op.NewShards
				kv.skvDB[op.Shard] = make(map[string]string)
				kv.sClerkLog[op.Shard] = make(map[int64]int)
				kv.copyDB(ns.ShardDB, kv.skvDB[op.Shard])
				kv.copyCL(ns.ClerkLog, kv.sClerkLog[op.Shard])
			}
		case request:
			//raft.ShardInfo.Printf("GID:%d cfg:%2d leader:%6v | Shard:%2d have{%v}\n", kv.gid, kv.cfg.Num, kv.leader, op.Shard, kv.shards)
			if ind, ok := kv.sClerkLog[op.Shard][op.Clerk]; ok && ind >= op.CmdIndex {
				//如果clerk存在，并且该指令已经执行，啥也不做
				raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|op{%v}have done! index:%4d \n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)

			}else if !kv.haveShard(op.Shard) || !kv.shardInHand(op.Shard) || kv.beSending(op.Shard){
				//本集群不再负责这个shard
				//或者本集群还没收到这个shard==>在start命令时，才从follower转为leader，因此跳过了executeOp一开始的筛选
				raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|op{%v}no responsible! index:%4d \n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)

				msgToChan = noLongerHandleThis
			}else{
				//执行指令
				raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|apply op{%v} index:%4d\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)

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

		ch, ok := kv.msgCh[index];
		if ok{
			ch <- msgToChan
		}else if kv.leader{
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| command{%v} no channel index:%2d!\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)
		}
		//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|finish a command\n", kv.gid, kv.me, kv.cfg.Num, kv.leader)

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
	//判断集群是否负责这个shard
	_, ok := kv.shards[shard]
	return ok
}

func (kv *ShardKV)shardInHand(shard int) bool{
	//判断这个shard收到没有
	_, ok := kv.skvDB[shard]
	return ok
}

func (kv *ShardKV)beSending(shard int) bool{
	//shard是否在发送过程中
	_, ok := kv.shardUnderSend[shard]
	return ok
}

func (kv *ShardKV)notSending(shard int){
	//shard是否在发送过程中
	kv.shardUnderSend[shard]--
	if kv.shardUnderSend[shard] == 0{
		delete(kv.shardUnderSend, shard)
	}
}



func (kv *ShardKV)needShard()(res []int){
	res = make([]int, 0)
	for shard, _ := range kv.shards{
		if _, ok := kv.skvDB[shard]; !ok{
			res = append(res, shard)
		}
	}
	return
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

func (kv *ShardKV) copyShard(src map[int]struct{})(des map[int]struct{}){
	des = make(map[int]struct{})
	for k,v := range src{
		des[k] = v
	}
	return
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

	//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| isop command{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op)

	if isOp{
		kv.mu.Lock()
		//判断指令需要的shard加载完成没有
		if kv.leader{
			if !kv.haveShard(op.Shard) || !kv.shardInHand(op.Shard) || kv.beSending(op.Shard){
				raft.ShardInfo.Printf("GID:%2d me%2d cfg:%2d | Do not responsible for this shard %2d\n", kv.gid, kv.me, kv.cfg.Num, op.Shard)
				raft.ShardInfo.Printf("responsible:{%v} need{%v}\n", kv.shards, kv.needShard())

				//raft.ShardInfo.Printf("db:%v\n", kv.skvDB)
				raft.ShardInfo.Printf("cfg:%d ==> %v\n\n", kv.cfg.Num, kv.cfg.Shards)
				kv.mu.Unlock()
				return
			}
			if ind, ok := kv.sClerkLog[op.Shard][op.Clerk]; ok && ind >= op.CmdIndex{
				//指令已经执行过
				raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d | Command{%v} have done before\n", kv.gid, kv.me, kv.cfg.Num, op)
				wrongLeader, wrongGroup = false, false
				kv.mu.Unlock()
				return
			}
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
	//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| Start. command{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op)

	index, _, isleader = kv.rf.Start(op)

	kv.mu.Lock()
	kv.leader = isleader

	if !isleader{
		//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| Sorry, I am not leader. command{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op)
		kv.mu.Unlock()
		return
	}
	wrongLeader = false

	ch := make(chan bool, 1)
	kv.msgCh[index] = ch
	kv.mu.Unlock()
	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| command{%v} begin! index:%2d\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)

	select {
	case <- time.After(RPC_CALL_TIMEOUT):
		wrongLeader = true
		raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| command{%v} index:%2d timeout!\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)
	case res := <- ch:
		if res == noLongerHandleThis{
			//不再负责这个shard
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| exclude command{%v} index:%2d!\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)
			wrongGroup = true
		}else{
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| command{%v} index:%2d Done!\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, op, index)
		}
	}
	//raft.ShardInfo.Printf("GID:%2d me:%2d | command{%T} Done!\n", kv.gid, kv.me, op)
	go kv.closeCh(index)
	return
}

//===============================================================================
//获取配置信息
//===============================================================================

func (kv *ShardKV) parseCfg(){
	//从cfg中生成待发送列表、待接收列表
	//凡是我有的且不属于我的，都发送出去
	shards := make(map[int]struct{})

	//此时cfg已经更新
	for shard, gid := range kv.cfg.Shards{
		if gid == kv.gid {
			shards[shard] = struct{}{}
		}
	}


	kv.shards = shards
	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| New shards:%v\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, kv.shards)

	if !kv.leader{
		//只有leader需要维护待发送列表
		return
	}

	shardToSend := make([]int, 0) //待发送列表
	for shard, _ := range kv.skvDB{
		if !kv.haveShard(shard){
			shardToSend = append(shardToSend, shard)
		}
	}
	kv.shardToSend = shardToSend
	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| New shardToSend:%v\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, kv.shardToSend)

}

func (kv *ShardKV) getCfg(start bool){
	//调用函数默认有锁
	var cfg shardmaster.Config
	for {
		if start{
			cfg = kv.sm.Query(1)
			if cfg.Num < 1{
				time.Sleep(time.Duration(5) * time.Millisecond)
				continue
			}
		}else{
			cfg = kv.sm.Query(-1)
		}
		if !kv.leader || cfg.Num <= kv.cfg.Num{
			//最新，返回
			return
		}
		//配置更新
		raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v me:%2d| Update config to %d\n", kv.gid, kv.cfg.Num, kv.leader, kv.me, cfg.Num)
		//raft.ShardInfo.Printf("New shard assign:%v\n", cfg.Shards)

		//同步新配置
		op := Op{
			newConfig,
			1,
			"",
			"",
			"",
			1,
			1,
			NewConfig{cfg,
								false},
			NewShards{}}

		if start {
			//第一次加载配置，直接初始化.
			op.NewConfig.Initial = true
		}

		kv.mu.Unlock()

		wl, wg := kv.executeOp(op)
		kv.mu.Lock()

		if !wl && !wg {
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:  true| Update to cfg:%2d successful!\n", kv.gid, kv.me, kv.cfg.Num, kv.cfg.Num)
			//更新配置成功，返回
			//可以保证，获取了配置1,才会去正常获取其他配置。
			kv.broadShard()
			return
		}
	}
}

func (kv *ShardKV) checkCfg(startFromOne bool){
	//周期性检查配置信息
	start := startFromOne
	for{
		isleader := kv.checkLeader()
		kv.mu.Lock()
		oldleader := kv.leader
		oldcfg := kv.cfg.Num
		kv.leader = isleader
		if kv.leader{
			//只有leader会检查配置
			kv.getCfg(start)
			if start{
				start = false
			}
		}
		//检查自己是否是Leader
		//由follower转为leader后，检查待发送列表
		if kv.leader != oldleader && kv.leader && oldcfg == kv.cfg.Num{
			//由follower转为leader
			//检查待发送列表是否为空，并发送
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d | follower turn to leader!\n", kv.gid, kv.me, kv.cfg.Num)
			//在sendShard执行前(需要kv.mu)，已经修改了kv.leader
			//变为leader，生成待发送列表
			//同一个配置转换leader，继续未完成的发送
			kv.parseCfg()
			//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d | parse done!\n", kv.gid, kv.me, kv.cfg.Num)
			kv.broadShard()
			//raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d | broad done!\n", kv.gid, kv.me, kv.cfg.Num)
		}
		kv.mu.Unlock()
		time.Sleep(Cfg_Get_Time)
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
	StaleCfg 	bool//发送方配置信息太老了
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
	reply.StaleCfg = true

	kv.leader = isleader

	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| receive shard %2d from gid:%2d cfg:%2d need{%v} args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, args.Shard, args.GID, args.CfgNum, kv.needShard(), args)

	if args.CfgNum < kv.cfg.Num {
		kv.mu.Unlock()
		return
	}
	reply.StaleCfg = false
	if !kv.leader || args.CfgNum > kv.cfg.Num || !kv.haveShard(args.Shard){
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
	//if args.CfgNum > kv.cfg.Num{
	//	//更新
	//	kv.getCfg()
	//}

	//raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| get shard %2d from gid:%2d\n", kv.gid, kv.cfg.Num, kv.leader, args.Shard, args.GID)
	//raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| %v\n", kv.gid, kv.cfg.Num, kv.leader, kv.newAddShards)
	replace := false
	if kv.shardInHand(args.Shard) {
		//已经接收过了，返回成功
		kv.mu.Unlock()
		reply.WrongLeader, reply.StaleCfg = false, false
		return
	}
	//if kv.shardInHand(args.Shard) {
	//	//已经接收过了，返回成功
	//	flag := true
	//	//TestChallenge1Concurrent
	//	for ck, ind := range args.ClerkLog{
	//		if in, ok := kv.sClerkLog[args.Shard][ck]; !ok || ind > in{
	//			flag = false
	//			replace = true
	//			break
	//		}
	//	}
	//	if flag{
	//		kv.mu.Unlock()
	//		reply.WrongLeader = false
	//		return
	//	}
	//}

	kv.mu.Unlock()
	//接收到shard
	//同步
	op := Op{newShard,
			args.Shard,
			"",
			"",
			"",
			-1,
			-1,
			NewConfig{},
			NewShards{args.ShardDB,args.ClerkLog, replace}}


	wrongLeader, wrongGroup := kv.executeOp(op)

	if !wrongLeader && !wrongGroup{
		//指令执行成功
		//执行成功，删掉待接收shard
		//该shard已经在执行指令时删除
		//delete(kv.newAddShards, args.Shard)
		reply.WrongLeader = false
		raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| add new shard %d done! \n", kv.gid, kv.cfg.Num, kv.leader, args.Shard)
	}
	if wrongGroup{
		reply.StaleCfg = true
	}
	return

}

func (kv *ShardKV) broadShard(){
	//调用者默认有锁，广播需要迁移的shard
	for _, shard := range kv.shardToSend{
		//kv.shardUnderSend[shard] = struct{}{}
		go kv.sendShard(shard, kv.cfg.Num)
	}
	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v|Begin to transfer {%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, kv.shardToSend)
}

func (kv *ShardKV) sendShard(shard int, cfg int){

	//旧集群发送shard逻辑
	for {
		kv.mu.Lock()

		if !kv.leader || cfg != kv.cfg.Num || !kv.shardInHand(shard){
			//配置已过期，或者该shard已发送，返回
			//即便两次配置都需要发送这个shard
			//但是新配置肯定也发起了新的goroutine
			//delete(kv.shardUnderSend, shard)
			kv.mu.Unlock()
			return
		}

		gid := kv.cfg.Shards[shard]
		if gid == kv.gid{
			//delete(kv.shardUnderSend, shard)
			kv.mu.Unlock()
			return
		}

		kvDB := make(map[string]string)
		ckLog := make(map[int64]int)
		kv.copyDB(kv.skvDB[shard], kvDB)
		kv.copyCL(kv.sClerkLog[shard], ckLog)


		args := MoveShardArgs{cfg, shard, kvDB, ckLog, kv.gid}

		if servers, ok := kv.cfg.Groups[gid]; ok {
			// try each server for the shard.
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| transfer shard %2d to gid:%2d args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, shard, gid, args)
			for si := 0; si < len(servers); si++ {
				srv := kv.make_end(servers[si])
				var reply MoveShardReply

				kv.shardUnderSend[shard]++
				kv.mu.Unlock()

				ok := srv.Call("ShardKV.MoveShard", &args, &reply)

				kv.mu.Lock()

				if !kv.leader{
					//delete(kv.shardUnderSend, shard)
					kv.notSending(shard)
					kv.mu.Unlock()
					return
				}
				if ok && reply.StaleCfg{
					//配置信息太旧
					//等待更新配置后发送
					//delete(kv.shardUnderSend, shard)
					kv.notSending(shard)
					kv.mu.Unlock()
					return
				}

				if ok && reply.WrongLeader == false {
					//发送完一个，在记录中删除
					//交付日志时会删除
					// delete(kv.shardToSend, shard)
					//清除该shard数据
					//delete(kv.skvDB, shard)
					//delete(kv.sClerkLog, shard)

					raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| OK! transfer shard %2d to gid:%2d args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, shard, gid, args)

					//迁移shard期间，不允许修改对应的数据库。
					kv.mu.Unlock()

					//同步发送成功日志
					op := Op{newSend,
						shard,
						"",
						"",
						"",
						-1,
						-1,
						NewConfig{},
						NewShards{}}
					wl, wg := kv.executeOp(op)
					//调用函数默认有锁情况下
					//必须用goroutine
					//否则run->sendShard->executeOp->start等待rf.mu->阻塞executeOp->阻塞sendShard->阻塞run
					//run阻塞->raft无法交付日志->ApplyMsg 一直占用rf.mu->start无法获取rf.mu
					//死锁
					//go kv.executeOp(op)

					kv.mu.Lock()
					kv.notSending(shard)
					if !wl && !wg {
						//同步成功
						kv.mu.Unlock()
						return
					}
					//同步失败，继续

					break
				}else{
					kv.notSending(shard)
				}
			}
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| Fail to transfer shard %2d to gid:%2d args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, shard, gid, args)
		}

		kv.mu.Unlock()
		//发送失败，等等再尝试
		time.Sleep(Send_Shard_Wait)
	}
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
	var cfg shardmaster.Config

	if dec.Decode(&db) != nil || dec.Decode(&cl) != nil || dec.Decode(&cfg) != nil{
		raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v| KV Failed to recover by snapshot!\n", kv.gid, kv.me, kv.leader)
	}else{
		kv.skvDB = db
		kv.sClerkLog = cl
		kv.cfg = cfg
		raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| KV recover from snapshot successful! \n", kv.gid, kv.me, kv.cfg.Num, kv.leader)
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

//	raft.ShardInfo.Printf("GID:%2d me:%2d leader:%6v cfg:%2d| Begin snapshot! \n", kv.gid, kv.me, kv.leader, kv.cfg.Num)
	rawSnapshot := kv.encodeSnapshot()
	go func() {kv.rf.TakeSnapshot(rawSnapshot, index, term)}()
}

func (kv *ShardKV) encodeSnapshot() []byte {
	//调用者默认拥有kv.mu
	w := new(bytes.Buffer)
	enc := labgob.NewEncoder(w)
	enc.Encode(kv.skvDB)
	enc.Encode(kv.sClerkLog)
	enc.Encode(kv.cfg)
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