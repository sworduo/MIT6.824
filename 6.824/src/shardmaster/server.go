package shardmaster

import (
	"kvraft"
	"raft"
	"time"
)
import "labrpc"
import "sync"
import "labgob"

const (
	//用于调整shard，详见navieAssign函数
	tless = 0
	tok = 1
	tplus = 2
	tmore = 3
	//该日志对应的操作
	join = "join"
	leave= "leave"
	move = "move"
	query = "query"
)

type ShardMaster struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	// Your data here.

	configs []Config // indexed by config num

	g2shard map[int][]int //每个group对应的shard

	clerkLog map[int64]int //记录已经执行的clerk的命令，避免重复执行。
	msgCh map[int]chan struct{} //消息通知
}


type Op struct {
	// Your data here.
	Clerk int64
	CmdIndex int
	Operation string //join, leave, move, query
	//join
	Servers map[int][]string // new GID -> servers mappings
	//leave
	GIDs []int
	//move
	//此时GID为GIDs[0]
	Shard int
	//query
	QueryNum int


}

func (sm *ShardMaster) getLastCfg() *Config{
	//必须返回的是指针
	//否则，数组是值复制
	//a := [5]int{1,2,3}
	//b := a
	//b[0] = 100
	//a并没有更改，还是123.(config.shard是数组)
	return &sm.configs[len(sm.configs)-1]
}

func (sm *ShardMaster) addConfig(){
	//调用函数必须有锁

	//单纯复制config并加
	//不做额外操作
	oldCfg := sm.getLastCfg()
	newShard := oldCfg.Shards //数组是值复制
	newGroup := make(map[int][]string)//map是引用复制
	for k,v := range oldCfg.Groups{
		newGroup[k] = v
	}
	sm.configs = append(sm.configs, Config{oldCfg.Num+1, newShard, newGroup})
}

func (sm *ShardMaster) cutG2shard(gid int) []int{
	//减少g2shard
	return sm.g2shard[gid][:len(sm.g2shard[gid])-1]
}

func (sm *ShardMaster) getLastShard(gid int) int{
	//获取某个group对应的最后一个shard
	return sm.g2shard[gid][len(sm.g2shard[gid])-1]
}

func (sm *ShardMaster) navieAssign(){
	//仅在有锁的情况调用这个函数

	//最笨的任务分配方式：
	//设shard总数为s，集群总数为g，那么每个集群至少应该有t=floor(s/g)个shard，且理想情况下，每个集群应该只有t或者t+1个shard。
	//然而由于move的情况，有的集群可能会有t+n(n>=2)个shard，同样的有的集群可能会有t-n(n<=1)个shard，导致shard分配不均衡。
	//记录四种情况：tless=0, tok=1, tplus=2, tmore=3，用于调整shard

	//暂时只用于shard > groupnum 的情况，如果集群数量多于shard数量，会陷入死循环
	//gmap := make([][]int, 4)
	//for _, g := range gmap{
	//	g = make([]int, 0)
	//}
	if len(sm.g2shard) == 0{
		//所有机器都leave了
		return
	}
	gmap := [4][]int{}

	cfg := sm.getLastCfg()
	shardToAssigh := make([]int, 0)
	for shard, gid := range cfg.Shards{
		if gid == 0{
			//记录还未被分配的shard
			shardToAssigh = append(shardToAssigh, shard)
		}
	}

	share := NShards / len(sm.g2shard) //这里的share等于上面分析的t
	for gid, shards := range sm.g2shard{
		l := len(shards)
		switch l {
		case share:
			gmap[tok] = append(gmap[tok], gid)
		case share + 1:
			gmap[tplus] = append(gmap[tplus], gid)
		default:
			if l < share {
				gmap[tless] = append(gmap[tless], gid)
			} else {
				gmap[tmore] = append(gmap[tmore], gid)
			}
		}
	}
	for len(gmap[tmore]) != 0{
		//清空tmore
		//平衡shard数量分配过多的group
		gid := gmap[tmore][0]
		moreShard := sm.getLastShard(gid)
		sm.g2shard[gid] = sm.cutG2shard(gid)
		shardToAssigh = append(shardToAssigh, moreShard)
		if len(sm.g2shard[gid]) == share + 1{
			gmap[tplus] = append(gmap[tplus], gid)
			gmap[tmore] = gmap[tmore][1:]
		}
	}



	//有新的shard需要分配
	//一般是有group leave了
	for len(shardToAssigh) != 0{
		curShard := shardToAssigh[0]
		shardToAssigh = shardToAssigh[1:]
		if len(gmap[tless]) != 0{
			gid := gmap[tless][0]
			cfg.Shards[curShard] = gid
			sm.g2shard[gid] = append(sm.g2shard[gid], curShard)
			if len(sm.g2shard[gid]) == share{
				//判断该gid所分配的shard是否达到最小要求t
				gmap[tok] = append(gmap[tok], gid)
				gmap[tless] = gmap[tless][1:]
			}
		}else{
			//给tok的group分配shard
			gid := gmap[tok][0]
			cfg.Shards[curShard] = gid
			sm.g2shard[gid] = append(sm.g2shard[gid], curShard)
			gmap[tok] = gmap[tok][1:]
			gmap[tplus] = append(gmap[tplus], gid)
		}
	}
	//raft.ShardInfo.Printf("ShardMaster:%2d gmap:{%v} tless:{%v} tok:{%v} tplus:{%v} tmore:{%v} g2shard:{%v}\n", sm.me, gmap, gmap[tless], gmap[tok], gmap[tplus], gmap[tmore], sm.g2shard)
	//raft.ShardInfo.Printf("ShardMaster:%2d |%v\n", sm.me, sm.configs)
	for len(gmap[tless]) != 0{
		//极端情况是所有group对应的shard数量都是t
		//当tless非空时，tplus也非空，注意，tmore在上面已经清空了
		//当保证shard数量永远大于group数量时，不可能出现gmap[tless]非空而gmap[plus]为空的情况
		gid := gmap[tplus][0]
		gmap[tplus] = gmap[tplus][1:]
		curShard := sm.getLastShard(gid)
		sm.g2shard[gid] = sm.cutG2shard(gid)
		gidless := gmap[tless][0]
		cfg.Shards[curShard] = gidless
		sm.g2shard[gidless] = append(sm.g2shard[gidless], curShard)
		if len(sm.g2shard[gidless]) == share {
			gmap[tless] = gmap[tless][1:]
		}
	}
}

func (sm *ShardMaster) Join(args *JoinArgs, reply *JoinReply) {
	// Your code here.
	raft.ShardInfo.Printf("ShardMaster:%2d join:%v\n", sm.me, args.Servers)
	op := Op{args.Clerk,args.Index,join,args.Servers,[]int{},0,0}
	reply.WrongLeader = sm.executeOp(op)
	return
}

func (sm *ShardMaster) Leave(args *LeaveArgs, reply *LeaveReply) {
	// Your code here.
	raft.ShardInfo.Printf("ShardMaster:%2d leave:%v\n", sm.me, args.GIDs)
	op := Op{args.Clerk,args.Index,leave,map[int][]string{},args.GIDs,0,0}
	reply.WrongLeader = sm.executeOp(op)
	return
}

func (sm *ShardMaster) Move(args *MoveArgs, reply *MoveReply) {
	// Your code here.
	raft.ShardInfo.Printf("ShardMaster:%2d move shard:%v to gid:%v\n", sm.me, args.Shard, args.GID)
	op := Op{args.Clerk,args.Index,move,map[int][]string{},[]int{args.GID},args.Shard,0}
	reply.WrongLeader = sm.executeOp(op)
	return
}

func (sm *ShardMaster) Query(args *QueryArgs, reply *QueryReply) {
	// Your code here.
	//必须要同步
	//因为测试时，开始配置中11更新之后的query指令期待收到配置11
	//如果不同步就返回配置，可能配置11的日志还在同步中还未执行，因此返回配置10,因此出错
	op := Op{args.Clerk,args.Index,query,map[int][]string{},[]int{},0,args.Num}
	reply.WrongLeader = sm.executeOp(op)
	if !reply.WrongLeader{
		sm.mu.Lock()
		defer sm.mu.Unlock()
		if op.QueryNum == -1 || op.QueryNum > sm.configs[len(sm.configs)-1].Num{
			//所要求的config比拥有的config大，返回最新的config
			reply.Config = *sm.getLastCfg()
		}else{
			reply.Config = sm.configs[op.QueryNum]
		}
	}
	return
}

func (sm *ShardMaster) executeOp(op Op) (res bool){
	sm.mu.Lock()
	//命令已经执行过了
	if index, ok := sm.clerkLog[op.Clerk]; ok{
		if index >= op.CmdIndex{
			sm.mu.Unlock()
			return false
		}
	}
	sm.mu.Unlock()

	index, _, isLeader := sm.rf.Start(op)
	if !isLeader{
		return true
	}
	//raft.ShardInfo.Printf("Shard:%2d | receive %v index %d\n", sm.me, op.Operation, index)
	//新建ch再放入msgCh的好处是，下面select直接用ch即可
	//而不是直接等待kv.msgCh[index]
	ch := make(chan struct{})
	sm.mu.Lock()
	sm.msgCh[index] = ch
	sm.mu.Unlock()

	select {
	case <- time.After(raftkv.WaitPeriod):
		//raft.ShardInfo.Printf("Shard:%2d | operation %v index %d timeout!\n", sm.me, op.Operation, index)
		res = true
	case <- ch:
		//raft.ShardInfo.Printf("Shard:%2d | operation %v index %d Done!\n", sm.me, op.Operation, index)
		res = false
	}
	go sm.closhCh(index)
	return
}

//
// the tester calls Kill() when a ShardMaster instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (sm *ShardMaster) Kill() {
	sm.rf.Kill()
	raft.ShardInfo.Printf("ShardMaster:%2d | I am died\n", sm.me)
	// Your code here, if desired.
}

// needed by shardkv tester
func (sm *ShardMaster) Raft() *raft.Raft {
	return sm.rf
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via raft to
// form the fault-tolerant shardmaster service.
// me is the index of the current server in servers[].
//
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardMaster {
	sm := new(ShardMaster)
	sm.me = me

	sm.configs = make([]Config, 1)
	sm.configs[0].Groups = map[int][]string{}

	labgob.Register(Op{})
	sm.applyCh = make(chan raft.ApplyMsg)
	sm.rf = raft.Make(servers, me, persister, sm.applyCh)
	sm.g2shard = make(map[int][]int)
	sm.msgCh = make(map[int]chan struct{})
	sm.clerkLog = make(map[int64]int)
	// Your code here.
	raft.ShardInfo.Printf("ShardMaster:%2d |Create a new shardmaster!\n", sm.me)
	go sm.run()

	return sm
}

func (sm *ShardMaster) run(){
	for msg := range sm.applyCh{
		sm.mu.Lock()
		index := msg.CommandIndex
		op := msg.Command.(Op)

		if ind, ok := sm.clerkLog[op.Clerk]; ok && ind >= op.CmdIndex{
			//命令已经执行过
		}else{
			switch op.Operation {
			case join:
				flag := false
				sm.addConfig()
				cfg := sm.getLastCfg()
				for gid, srvs := range op.Servers{
					if _, ok := sm.g2shard[gid]; !ok{
						//防止同样的server join两次
						flag = true
						cfg.Groups[gid] = srvs
						sm.g2shard[gid] = []int{}
					}
				}
				if flag{
					sm.navieAssign()
					raft.ShardInfo.Printf("ShardMaster:%2d | new config:{%v}\n", sm.me, sm.getLastCfg())
				}
			case leave:
				flag := false
				sm.addConfig()
				cfg := sm.getLastCfg()
				for _, gid := range op.GIDs{
					if _, ok := sm.g2shard[gid]; ok {
						flag = true
						for _, shard := range sm.g2shard[gid] {
							//0表示没有分配
							cfg.Shards[shard] = 0
						}
						delete(sm.g2shard, gid)
						delete(cfg.Groups, gid)
					}
				}
				if flag{
					sm.navieAssign()
					raft.ShardInfo.Printf("ShardMaster:%2d | new config:{%v}\n", sm.me, sm.getLastCfg())
				}
			case move:
				sm.addConfig()
				cfg := sm.getLastCfg()
				oldGid := cfg.Shards[op.Shard]
				//旧集群移去这个shard
				for ind, s := range sm.g2shard[oldGid]{
					if s == op.Shard{
						sm.g2shard[oldGid] = append(sm.g2shard[oldGid][:ind], sm.g2shard[oldGid][ind+1:]...)
						break
					}
				}
				gid := op.GIDs[0]
				cfg.Shards[op.Shard] = gid
				sm.g2shard[gid] = append(sm.g2shard[gid], op.Shard)
			case query:
			}
		}
		if ch, ok := sm.msgCh[index]; ok{
			//命令完成
			ch <- struct{}{}
		}
		sm.mu.Unlock()
	}
}

func (sm *ShardMaster) closhCh(index int){
	sm.mu.Lock()
	defer sm.mu.Unlock()
	close(sm.msgCh[index])
	delete(sm.msgCh, index)
}