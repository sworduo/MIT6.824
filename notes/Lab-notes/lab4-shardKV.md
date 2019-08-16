#	MIT6.824 Lab4-ShardKV
&emsp;&emsp;Lab2和Lab3构成基础分布式数据库的框架，实现多节点间的数据一致性，支持增删查改，数据同步和快照保存。然而，在实际应用中，当数据增长到一定程度时，若仍然使用单一集群服务所有数据，将会造成大量访问挤压到leader上，增加集群压力，延长请求响应时间。这是由于lab2和lab3所构成的分布式数据库的核心在于分布式缓存数据，确保在leader宕机后，集群仍然可用，并没有考虑集群负载问题，每时每刻，所有数据请求都集中在一台机器上，显而易见的，在数据访问高峰期，请求队列将会无比漫长，客户等待时间也会变长。一个非常直接的解决方法，就是将数据按照某种方式分开存储到不同的集群上，将不同的请求引流到不同的集群，降低单一集群的压力，提供更为高效、更为健壮的服务。Lab4就是要实现分库分表，将不同的数据划分到不同的集群上，保证相应数据请求引流到对应的集群。这里，将互不相交并且合力组成完整数据库的每一个数据库子集称为shard。在同一阶段中，shard与集群的对应关系称为配置，随着时间的推移，新集群的加入或者现有集群的离去，shard需要在不同集群之中进行迁移，如何处理好配置更新时shard的移动，是lab4的主要挑战。  

[我的实现](https://github.com/sworduo/MIT6.824)   

>很多时候，为了打log方便调bug，有些地方的实现会有点奇怪或者过于繁琐，忽略就好。

#	Lab4A-ShardMaster
&emsp;&emsp;Lab4A主要是解决配置更新时，重新划分shard的逻辑。要求以移动尽可能少的shard的方式将shard尽可能的平均分配到提供服务的集群中。总体结构和lab3类似，用一个主goroutine监听各种配置更新，包括Join(新集群加入)，leave(旧集群退出)，move(将集群A迁移到集群B)，query(查询特定的配置信息)。在这里，用一个列表保持所有的配置更新情况，可以通过query查询特定的配置信息，这对于lab4B非常重要。

##	数据结构
下面是我用到的变量：
*	configs []Config ===>保存每次的配置信息。
*	g2shard map[int][]int ===>记录每个group（集群）所对应的shard。
*	clerkLog map[int64]int ===>记录已经执行的clerk的命令，避免重复执行。
*	msgCh map[int]chan struct{} ===>命令执行成功与否的消息通知。

##	重新划分shard逻辑
&emsp;&emsp;这里，我用了比较笨的方式来划分shard。首先根据shard总数nShards和当前的集群数量numG，计算出每个集群所应该分配到的最小shard数量:

	shard=nShards / numG
	
很显然，由于要求尽可能平均的划分shard，因此，每个集群所分配到的shard要不就是share个，要不就是shard+1个。我使用大小为4的切片数组gmap来保存四种类型的group，第一种是group现有的shard小于share的group,存放于gmap[tless]中；第二种是等于shard的group，存放于gmap[tok]中；第三种是等于share+1的group，存放于gmap[tplus]中；第四种是多余share+1的group，存放于gmap[tmore]中。具体的流程如下：
1.	扫描新配置Shards数组中值为0的shard，在我的实现中，值为0的shard代表还未被分配的shard，存放于于shardToAssign里。
2.	扫描g2shards,计算出每个group（集群）所分配到的shard的数量，并将拥有响应shard数量的group存放于相应切片中。
3.	扫描gmap[tmore]切片，显然这部分group拥有多于shard+1个shard，应该减少这部分group持有的shard，并且分配给其他group。具体做法是，从这些group中抽取shard放到shardToAssign中，直到这些group所负责的shard数量为shard+1为止。
4.	遍历shardToAssign，将shard分配给gmap[tless]中的group，直到这些group分配到的shard都是share。如果没有gmap[tess],就分配给gmap[tok]。
5.	遍历gmap[tless]，如果该切片不为空，代表仍有group分配到的shard数量小于share，此时应将gmap[tmore]中group的shard分配给gamp[tless]中的group，出现这种状况，有可能是有集群离去，也有可能是shard总数恰好可以整除集群数量。


```go
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

```

##	处理底层raft递交的日志逻辑
&emsp;&emsp;这一部分的重点在于，如何处理重复加入的集群？比如如果集群101之前已经加入并且尚未离开，又提示集群101加入，应该怎么处理？在我的实现中，由于集群101已经存在，所以不做任何事情。  
```go
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

```

##	辅助函数
&emsp;&emsp;由于篇幅所限，有一部分辅助函数没有列出来，有兴趣的朋友可以点击[这里](https://github.com/sworduo/MIT6.824/blob/master/6.824/src/shardmaster/server.go)查看我的完整实现。

#	Lab4B-Sharded Key/Value Server
&emsp;&emsp;Lab4A小试牛刀，让我们复习了一下上层server和底层raft交互的方式和逻辑。接下来，Lab4B真正进入到了整个lab的精髓——shard迁移，可以说，lab4和lab3最大的改变就是多了配置更新时shard的迁移，然而正是这一点变动引入了非常复杂的边界条件。在真正动手前，需要我们了解需求，制定规则，设计框架，许多细微的东西都值得我们仔细斟酌，好生思量。我在写lab4B时，由于一开始的考虑不周，走了不少弯路，直到实在走不通时，才重新从结构层面审视我的框架设计，也因此，写的磕磕盼盼，费心费力。在后面我会分享一下我各种设计思路的碰壁和演变，在这里奉劝大家一句，写之前先想好。下面我简单分享一下最后通过测试的思路。

##	基本数据结构和函数
*	msgCh 	map[int]chan msgInCh===>管理某次请求是否执行成功的管道。
*	sClerkLog 	map[int]map[int64]int ===>每一个shard独立的管理客户请求。第一个map,shard到客户的映射，第二个map，客户到客户指令的映射。
*	skvDB 	map[int]map[string]string ===>每一个shard独立的管理数据库。第一个map，shard到相应的数据库，第二个map，key到value的映射。
*	sm *shardmaster.Clerk ===>用于获取配置的shardmaster
*	shards 	map[int]struct{} ===>当前配置集群所负责的shard
*	shardToSend 	[]int  ===>配置更新后需要发送给其他集群的shard
*	cfg 	shardmaster.Config  ===>记录当前配置信息
*	leader 	bool ===> 判断本server是不是leader
*	persister *raft.Persister
*	waitUntilMoveDone int ===>等待本配置发送shard/接收shard完成，只有发送/接收完shard后，才能更新新的配置
*	exitCh chan struct{} ===>结束管道

###	同步日志的种类
&emsp;&emsp;在这个设计中，每个集群的leader共有五种命令需要同步：
1.	request：正常的put、append、get请求。
2.	newConfig：代表更新到新的配置。
3.	newShard：代表接收到新的shard，将该shard增加到当前数据库中。
4.	newSend：代表发送新的shard，将该shard从数据库中移除。
5.	newLeader：follower转为leader，为应付raft论文中fugure8的情况，新leader上位后需要同步一条空指令。  

用于同步日志的指令op数据结构如下：
*	Type 	string //request or newConfig or newShard or newSend or newLeader
*	Shard 	int
*	Operation 	string //Put or Append or Get
*	Key 	string
*	Value 	string
*	Clerk 	int64
*	CmdIndex 	int
*	NewConfig NewConfig
*	NewShards NewShards  

其中：
```go
type NewShards struct{
	//接收到新的shard
	ShardDB 	map[string]string
	ClerkLog 	map[int64]int
}

type NewConfig struct{
	//新配置
	Cfg 	shardmaster.Config
}
```
其实也没必要这么麻烦，只不过当时newshard和newconfig结构体的内容改过好几次，所以就一直沿用结构体了。

###	一些辅助函数
具体实现看[这里](https://github.com/sworduo/MIT6.824/blob/master/6.824/src/shardkv/server.go)这里仅仅把函数名和功能列出来。
*	equal ==> 判断命令是否相等。由于会出现raft论文中figure8的情况，所以有的日志很可能会被其他日志覆盖掉，因此需要判断所执行的日志是否是发起的日志。
*	haveshard ==> 判断集群是否负责这个shard
*	shardInHand ==> 判断集群是否拥有这个shard。结合haveshard和shardInHand，可以让集群服务已经有的shard，而拒绝服务还未收到的shard。
*	needShard ==> 配置更新后，集群判断需要接收哪些shard。
*	convertToLeader ==> follower检测到自己转为leader后，需要同步一条空指令。应对raft论文figure8的情况。
*	copyDB ==> 复制map的内容到新的map。

##	配置更新
对于具体负责某个shard的集群：
1.	仅仅只有leader去获取配置信息，如果检测到配置更新，则会促发一次日志同步，更新整个集群的配置。
2.	在配置更新后，仅仅只有leader负责shard迁移和接收，每次迁移/接收成功一个shard，会促发一次日志同步，保证整个集群同步删除/新增shard。
3.	follower仅仅是响应leader的日志，进行相应的配置更新、迁移/接收shard，不负责与外界的任何交互。
4.	考虑到只有leader迁移shard，如果在迁移shard过程中leader宕机了，新leader上来后需要发送未发送完成的shard。  

对于负责不同shard的集群100，101：
1.	每个集群独立向shardmaster获取最新的配置信息。
2.	如果配置更新，那么采用push的方式进行shard迁移。比如集群100检测到配置更新后，需要将shard 1，2迁移到101，则由100主动发送给101，101等待接收shard 1，2。
3.	在每一轮配置中，每个集群必须接收完所有新的shard，并且发送完所有需要发送的shard后，才能开始新一轮配置更新。按序执行配置更新。

```go
func (kv *ShardKV) checkCfg(){
	//周期性检查配置信息

	for{
		select{
		case <- kv.exitCh:
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v <- close checkCfg!\n", kv.gid, kv.me, kv.cfg.Num, kv.leader)
			return
		default:
			isleader := kv.checkLeader()
			kv.mu.Lock()
			kv.convertToLeader(isleader)
			if kv.leader && kv.waitUntilMoveDone == 0{
				//只有leader并且这一轮配置的shard都发送/接收完时，才会检查配置
				kv.getCfg()
			}
			kv.mu.Unlock()
			time.Sleep(Cfg_Get_Time)
		}
	}
}

func (kv *ShardKV) getCfg(){
	//调用函数默认有锁
	var cfg shardmaster.Config
	//按序执行配置更新
	cfg = kv.sm.Query(kv.cfg.Num+1)
	for {
		if !kv.leader || cfg.Num <= kv.cfg.Num{
			//最新，返回
			return
		}
		//配置更新
		raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| Update config to %d\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, cfg.Num)
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
			NewConfig{cfg},
			NewShards{}}

		kv.mu.Unlock()

		wl, wg := kv.executeOp(op)
		kv.mu.Lock()

		if !wl && !wg {
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:  true| Update to cfg:%2d successful!\n", kv.gid, kv.me, kv.cfg.Num, kv.cfg.Num)
			//更新配置成功，返回
			kv.broadShard()
			return
		}
		//有新的cfg却更新失败，立即重复更新配置
	}
}

```

配置更新的日志同步后，执行配置更新的逻辑：
1.	更新配置
2.	更新shards
3.	计算出waitUntilMoveDone，该值等于本轮要发送的shard数量+本轮要接收的shard数量之和。只有该变量为0，才能促发新的配置信息检查  

```go
func (kv *ShardKV) parseCfg(){
	//从cfg中生成待发送列表、待接收列表
	//凡是我有的且不属于我的，都发送出去
	shards := make(map[int]struct{})

	newAdd := make(map[int]struct{})

	//此时cfg已经更新
	for shard, gid := range kv.cfg.Shards{
		if gid == kv.gid {
			shards[shard] = struct{}{}
			if !kv.shardInHand(shard){
				//需要接收的shard
				//kv.waitUntilMoveDone++
				newAdd[shard] = struct{}{}
			}
		}
	}

	kv.shards = shards
	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| New shards:%v\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, kv.shards)

	shardToSend := make([]int, 0) //待发送列表
	for shard, _ := range kv.skvDB{
		if !kv.haveShard(shard){
			shardToSend = append(shardToSend, shard)
		}
	}
	kv.shardToSend = shardToSend
	kv.waitUntilMoveDone = len(shardToSend) + len(newAdd) 

	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| total receive/send %d newAdd:{%v} | New shardToSend:%v\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, kv.waitUntilMoveDone, newAdd, kv.shardToSend)
}

```

##	迁移shard
1.	配置更新后，调用`broadShard`函数将需要发送的shard发送给新的集群。
2.	发送成功后，同步日志成功才会删除相应的shard。
3.	接收方接收shard后，同步日志成功才会将shard添加到自己的数据库中。  

```go
type PushShardArgs struct{
	CfgNum int
	Shard 	int
	ShardDB map[string]string
	ClerkLog map[int64]int
	GID int
}

type PushShardReply struct{
	WrongLeader 	bool
}
```

发送shard逻辑：
```go

func (kv *ShardKV) sendShard(shard int, cfg int){

	//旧集群发送shard逻辑
	for {
		kv.mu.Lock()

		if !kv.leader || !kv.shardInHand(shard) || kv.cfg.Num > cfg{
			kv.mu.Unlock()
			return
		}

		gid := kv.cfg.Shards[shard]
		kvDB := make(map[string]string)
		ckLog := make(map[int64]int)
		kv.copyDB(kv.skvDB[shard], kvDB)
		kv.copyCL(kv.sClerkLog[shard], ckLog)

		args := PushShardArgs{cfg, shard, kvDB, ckLog, kv.gid}

		if servers, ok := kv.cfg.Groups[gid]; ok {
			// try each server for the shard.
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| transfer shard %2d to gid:%2d args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, shard, gid, args)
			for si := 0; si < len(servers); si++ {
				srv := kv.make_end(servers[si])
				var reply PushShardReply

			
				kv.mu.Unlock()

				ok := srv.Call("ShardKV.PushShard", &args, &reply)

				kv.mu.Lock()

				if !kv.leader{
					kv.mu.Unlock()
					return
				}

				if ok && reply.WrongLeader == false {
					//发送完一个，在记录中删除
					//交付日志时会删除
					//清除该shard数据
					
					raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| OK! transfer shard %2d to gid:%2d args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, shard, gid, args)
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

					if wl{
						//不再是leader
						return
					}

					if !wl && !wg {
						//同步成功
						return
					}
					kv.mu.Lock()
					//同步失败，继续
					break
				}
			}
			raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| Fail to transfer shard %2d to gid:%2d args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, shard, gid, args)
		}

		kv.mu.Unlock()
		//发送失败，等等再尝试
		time.Sleep(Send_Shard_Wait)
	}
}
```

接收shard逻辑：
```go

func (kv *ShardKV) PushShard(args *PushShardArgs, reply *PushShardReply){

	//新集群接收shard逻辑
	kv.mu.Lock()

	raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| receive shard %2d from gid:%2d cfg:%2d need{%v} args{%v}\n", kv.gid, kv.me, kv.cfg.Num, kv.leader, args.Shard, args.GID, args.CfgNum, kv.needShard(), args)

	if kv.leader && args.CfgNum < kv.cfg.Num{
		//收到旧配置发来的shard
		//返回“接收成功”
		//因为本集群更新配置的前提，是收到了本配置需要的所有的shard
		reply.WrongLeader = false
		kv.mu.Unlock()
		return
	}
	if !kv.leader || args.CfgNum > kv.cfg.Num || !kv.haveShard(args.Shard){
		reply.WrongLeader = true
		kv.mu.Unlock()
		return
	}

	//raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| get shard %2d from gid:%2d\n", kv.gid, kv.cfg.Num, kv.leader, args.Shard, args.GID)
	//raft.ShardInfo.Printf("GID:%2d cfg:%2d leader:%6v| %v\n", kv.gid, kv.cfg.Num, kv.leader, kv.newAddShards)

	if kv.shardInHand(args.Shard) {
		//已经接收过了，返回成功
		kv.mu.Unlock()
		reply.WrongLeader = false
		return
	}

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
			NewShards{args.ShardDB,args.ClerkLog}}


	wrongLeader, wrongGroup := kv.executeOp(op)
	reply.WrongLeader = true
	if !wrongLeader && !wrongGroup{
		//指令执行成功
		reply.WrongLeader = false
		raft.ShardInfo.Printf("GID:%2d me:%2d cfg:%2d leader:%6v| add new shard %d done! \n", kv.gid, kv.me, kv.cfg.Num, kv.leader, args.Shard)
	}

	return

}
```

##	思考
1.	 如果leader在发送shard，同步日志前宕机怎么办？

	答：新leader选出后，同步空指令的同时，检测是否有shard还没发送成功，如果有，继续发送。因此接收方需要实现幂等性，防止同一个shard接收多次。
	
2.	如果leader在发送shard后，同步配置更新日志前宕机，新leader选出，在新leader检测到配置更新不再负责shard1前，接收到shard1的key的请求，并且执行之，怎么办？要注意，旧leader发送的shard中不含该key的请求。新leader发送的shard会被接收方丢弃。

	答：不会出现这样的情况。配置更新需要全部机器认可才会更新配置信息，因此，当leader发送shard时，代表配置更新这条日志已经完成同步，此时所有机器都已经更新了相应的配置。新leader上来后不会再接收该shard的消息。为了方便实现请求的幂等性，shard迁移的内容包括数据库和客户访问信息。
	
3.	集群如何区分服务哪些shard呢？

	答：在我的实现中，数据库的保存方式是shard到数据库的映射，因此迁移/接收shard只需要修改字典的对应项即可。因此，当shard迁移后，会删掉该项，当接收到新的shard后，会添加该shard。因此，只要检查请求中的shard是否存在于数据库字典中，就能判断是否服务这个shard。这样一来，可以在没接收到新shard的同时，服务已经有的shard。而对于配置1的特殊情况，他们的数据库直接新建。
	
4.	快照的内容？

	答：应该快照的内容：数据库、客户指令编号、最新的配置cfg。通过cfg，可以知道这个配置这个集群负责哪些shard。通过cfg和数据库的组合，可以知道当前配置应该发送哪些shard，也知道应该接收哪些shard。
	
5.	配置更新时，集群宕机问题？

	答：本设计的前提是基于集群一定可以恢复服务的，因此，如果某个集群在某个配置完全宕机永不恢复，那么，发送shard给该集群的集群A，以及等待从该集群接收新shard的集群B同样会陷入阻塞中。因为代码中要求每个集群发送/接收完shard后才能进行配置更新。
	
##	踩坑记录
1.	通知管道msgCh新建时需要设计容量大小为1，防止阻塞。这是考虑到一种情况，如果put超时，但是在该管道清除前，刚好执行了这个日志，并且往通知管道里塞东西，然而put函数这边没有接收方，因此造成阻塞。如果容量设计为1就不会出现这种情况。
2.	每个后台程序都需要接收结束管道exitCh。同样的，为了防止阻塞，我这里有2个后台程序，exitCh大小也应该设计为2。一开始我仅仅结束一个后台程序。另一个后台程序依旧在运行。因此，当这个集群“关闭”后，这个没有被杀死的后台疯狂运行，占用cpu资源，最后发生错误。
3.	还有不少引发死锁的坑，由于设计不同，而且原因比较复杂，并且对我的架构不熟悉的朋友可以很难get到关键点，加上比较懒，这方面就不多说了。
4.	某个test类似于raft的figure8问题，因此，在检测到follower转为leader后，我会同步一条空指令。这样造成的后果就是某条指令可能会被覆盖，因此，在执行指令的函数中需要加一个指令是否相同的判断。
5.	还有不少坑，但是写这篇文章的时候距离我完成lab4已经有一段时间了，一时想不起来，大家意会一下。
6.	实现完之后，觉得挺简单的，而且设计也非常直接，照本宣科就可以了。想想之前做了不少弯路，唏嘘。当然，这个实现有个问题，要求所有集群在所有配置都必须可用，否则，一旦某个集群在某个配置宕机，将会使得整个大集群阻塞。而我一开始的设计，就是希望可以允许某些集群在某些配置中宕机，允许跳过某些配置。但是，使用push shard方法，将会产生不可调和的冲突（pull shard是否会有这种冲突，没仔细思考过），过不了test case，只能作废。老老实实按序执行。


##	设计演变
1.	第一次设计，所有server包括follower都去获取配置信息。==>然而当leader宕机重新选举后，容易出现数据不一致问题，且不好解决，只能作废。老老实实按序执行。
2.	第二次设计，集群只有leader去检查配置更新，此时获取配置更新并不是按序获取，而是直接获取最新的配置，目的时为了容许某些集群在某些配置可以宕机，串行迁移shard。==>然而，这样实现会发生数据不一致的情况，有些put消失了，有些append了两次，不好管理。
3.	第三次设计，所有集群都按序执行配置更新，并行迁移shard，成功了。  

&emsp;&emsp;回过头来看看提示，应该逐一配置更新才对。提示这东西，刚看完题目要求看提示，只会把一些我当时觉得重要的记下来，比如map要复制什么的。有一些提示涉及实现上的要点，然而在真正实现之前我是不清楚的，因此忽略了。现在回过头来看，提示都很清楚了。

##	结果
```go
Test: static shards …
  ... Passed
Test: join then leave ...
  ... Passed
Test: snapshots, join, and leave ...
  ... Passed
Test: servers miss configuration changes...
  ... Passed
Test: concurrent puts and configuration changes...
  ... Passed
Test: more concurrent puts and configuration changes...
  ... Passed
Test: unreliable 1...
  ... Passed
Test: unreliable 2...
  ... Passed
Test: unreliable 3...
  ... Passed
Test: shard deletion (challenge 1) ...
  ... Passed
Test: concurrent configuration change and restart (challenge 1)...
  ... Passed
Test: unaffected shard access (challenge 2) ...
  ... Passed
Test: partial migration shard access (challenge 2) ...
  ... Passed
PASS
ok  	shardkv	175.306s

```

##	依旧存在的bug
&emsp;&emsp;这个实现偶尔会出现bug，引发bug的原因是，不同集群获取到的配置信息不同。比如，集群100和101获取的配置7是[100,100,101,102]，配置102获取的配置7是[102,100,100,101]，因此出现错误。我也不知道是啥原因引发的，猜测可能是对go语言语法不熟悉，有些地方是引用传递我却以为是值传递。

#	总结
&emsp;&emsp;Lab1熟悉语法，lab2将文字、结构、框架转化为代码，lab3给出提纲，自己思考脉络细节，lab4自己设计。总的来说，代码量减少，但是不断加深思考，从just's do it转化为how to do it，这种思想上的转变才是这个实验最大的收获。彻底理解一句话，一个程序员应该八成时间在思考，两成时间写代码。想好再写，事半功倍啊。

&emsp;&emsp;花费了大概五天的时间，换了三种设计思路，重构了两次，基本上就是 思考半天确定框架=>设计半天确保一致性=>写半天=>print大法调试半天=>发现现有设计的不可调和的矛盾=>重新思考半天 循环了两三次。不过当程序跑通时，就好像是放下了沉沉的担子，一下子轻松起来。有点像负重练习，给自己压力，当完成目标时，能收获200%的喜悦。

&emsp;&emsp;经验：在各种你觉得有用的地方print各种你觉得有用的信息，不要怕日志太多难以查错不好处理，只要你print的信息满足一定规律，可以很轻易地通过grep命令筛选出有用的信息。

&emsp;&emsp;如果不想管道阻塞，并且塞进的信息容许不被消费，那么可以将管道设计为1。说的就是kv执行命令后通过管道通知等待的函数指令执行成功。考虑到超时，但是管道还没回收的情况，此时管道没有接收者，因此，如果管道大小不设计为1，会阻塞。同样还有exitCh管道，要设计为1才行，因此该管道只能异步获取，而不能同步。管道双方信息交流，同步就设为0，异步就设计相应大小。