#	MIT6.824 Lab4-ShardKV
&emsp;&emsp;Lab2和Lab3构成基础分布式数据库的框架，实现多节点间的数据一致性，支持增删查改，数据同步和快照保存。然而，在实际应用中，当数据增长到一定程度时，若仍然使用单一集群服务所有数据，将会造成大量访问挤压到leader上，增加集群压力，延长请求响应时间。这是由于lab2和lab3所构成的分布式数据库的核心在于分布式缓存数据，确保在leader宕机后，集群仍然可用，并没有考虑集群负载问题，每时每刻，所有数据请求都集中在一台机器上，显而易见的，在数据访问高峰期，请求队列将会无比漫长，客户等待时间也会变长。一个非常直接的解决方法，就是将数据按照某种方式分开存储到不同的集群上，将不同的请求引流到不同的集群，降低单一集群的压力，提供更为高效、更为健壮的服务。Lab4就是要实现分库分表，将不同的数据划分到不同的集群上，保证相应数据请求引流到对应的集群。这里，将互不相交并且合力组成完整数据库的每一个数据库子集称为shard。在同一阶段中，shard与集群的对应关系称为配置，随着时间的推移，新集群的加入或者现有集群的离去，shard需要在不同集群之中进行迁移，如何处理好配置更新时shard的移动，是lab4的主要挑战。  

[我的实现](https://github.com/sworduo/MIT6.824)   

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
&emsp;&emsp;Lab4A小试牛刀，让我们复习了一下上层server和底层raft交互的方式和逻辑。接下来，Lab4B真正进入到了整个lab的精髓——shard迁移，可以说，lab4和lab3最大的改变就是多了配置更新时shard的迁移，然而正是这一点改变引入了非常复杂的边界条件。在真正动手前，需要我们了解需求，制定规则，设计框架，许多细微的东西都值得我们仔细斟酌，好生思量。我在写lab4B时，由于一开始的考虑不周，走了不少弯路，直到实在走不通时，才重新从结构层面审视我的框架设计，也因此，写的磕磕盼盼，费心费力。在后面我会分享一下我各种设计思路的碰壁和演变，在这里奉劝大家一句，写之前先想好。下面我简单分享一下最后通过测试的思路：  