#	[Replication: preventing divergence](http://book.mixu.net/distsys/replication.html)
参考1:[袖珍分布式系统四](https://www.jianshu.com/p/558974ede572)  
参考2:[理解这两点，也就理解了paxos协议的精髓](https://blog.csdn.net/qq_35440678/article/details/78080431)  
参考3:[Paxos协议超级详细解释+简单实例](https://blog.csdn.net/cnh294141800/article/details/53768464)  

![cute](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter4-head.jpg "cute")

#	Replication
replication问题是分布式系统中最重要的问题，现在有非常多实现replicatoin的算法，因各自的考虑和取舍而表现出极大的差异。接下来，本文将讨论所有这些replication算法的共通之处，而不会针对某一个算法深入讲解。本文将会聚焦于以下四个方面：
*	leader election,
*	failure detection,
*	mutual exclusion,
*	consensus and global snapshots
我们首先得知道Replicatoin问题本质上是一个group communication问题。在单机操作系统上，进程间有多种通信方式，共享内存，消息机制等等，然而在分布式系统中，显然系统中的节点只能通过网络通信来进行信息交互。既然设计到网络通信，那么我们首先来考虑一下同步和异步两种通信模型：
![SAcom](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter4-SAcom.png "同/异步通信模型")

我们可以把复制步骤分为4步：
*	(Request) The client sends a request to a server
*	(Sync) The synchronous portion of the replication takes place
*	(Response) A response is returned to the client
*	(Async) The asynchronous portion of the replication takes place
基于以上四个步骤，又可以分为同步复制和异步复制。

##	Synchronous replication
同步复制（synchronous replication）又称为：active, or eager, or push, or pessimistic replication，其原理如下图：
![syn](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter4-syn.png "同步通信模型")
具体步骤是这样的：
*	1、client发送请求
*	2、s1接受到请求，然后阻塞，并将同样的请求发送给其他**所有**主机
*	3、s1接到其他**所有**主机的答复之后，才最终回复client
从上述的过程我们可以看出：
*	这个系统遵循木桶原理，也就是说，系统的性能由最慢的那个节点决定。
*	系统对网络延迟非常敏感，每一次写入都要访问所有的节点。
*	一旦有一个server宕机了，系统只能提供读服务。
这种模型的好处在于，当client收到回复时，client可以保证此时系统中所有节点都进行了相应的更改。然而有一个非常致命的地方，就是系统几乎不能容忍有节点宕机的情况，并且对延迟非常敏感，使得一次操作可能会非常耗时，且不一定能成功。  

##	Asynchronous replication
![asyn](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter4-asyn.png "异步通信模型")
异步复制在master接收到请求之后，只是做一些简单的local处理，然后直接返回，不阻塞客户端。在这之后再将本次修改同步到网络中的其他节点。  
从性能角度看，因为复制是异步进行的，延迟非常小，但是系统的一致性是弱一致的，最重要的是系统无法保证数据的持久性，因为写入master的数据，可能在master复制给其他slaver的前，master就故障了，此时数据的就丢失了。

#	An overview of major replication approaches
讲完同步和异步复制两个思路后，我们来看一些稍微具体的算法，有许多方法来对复制算法分类，除了上面的同步和异步外，还能这么分：
*	Replication methods that prevent divergence (single copy systems) and
*	Replication methods that risk divergence (multi-master systems)
第一类系统遵循着“"behave like a single system”的原则，当partial failures发生的时候，算法能保证系统中只有一份数据是有效的，实现single-copy consistency的算法主要有：
*	1n messages (asynchronous primary/backup)
*	2n messages (synchronous primary/backup)
*	4n messages (2-phase commit, Multi-Paxos)
*	6n messages (3-phase commit, Paxos with repeated leader election)
这些算法的不同之处在于他们考虑的types of faults不同，上面简单的通过算法中交换消息的数量进行了划分，这么划分的原因是因为作者尝试回答：what are we buying with the added message exchanges?  
先看一张图：
![google](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter4-goo.png "aha")
上图来自：Google App Engine的co-founder Ryan Barrett在2009年的google i/o上的演讲《Transaction Across DataCenter》。  
consistency, latency, throughput, data loss and failover characteristics 归根结底来自于两个不同的复制方法：同步还是异步。下面我们具体看下每一种复制方法。

##	Primary/backup replication
>All updated are performed on the primary, and a log of operations (or alternatively, changes) is shipped across the network to the backup replicas.

这里的Primary/backup个人认为可以理解为master/slave，据说不用master/slave是因为这组单词被某些人投诉了。。。
最基本的一种复制方法，在复制log上有两种方法：
*	asynchronous primary/backup replication and
*	synchronous primary/backup replication
同步方法需要涉及到两个消息（"update" + "acknowledge receipt"），而异步则只有一个("update")。但是在mysql中，即使是同步复制也不能完全保证数据的一致性。真实场景中，有几种典型的P/B不合时宜的错误：
*	主机发送修改给slave，slave接受了修改，然而主机此时挂了，这时候主机不是最新的，而slave却是最新的。
*	由于网络原因，主机挂了，然后选择新的slave作为主机，然而网络突然又好了，此时变成了2台主机，该怎么选。
*	由于网络原因，一些slave接受了更新，一些slave没有接受到更新，这时候就矛盾了。
这里的P/B模式本质上来说就是只在一个master上跟client进行信息交互，然后再由master将信息更新扩散到其他slave节点上，好处在于，这时候不用考虑时钟一致性什么的，因为这时候系统上流传的信息都来自于同一台master，因此操作一定是按照某种可以预测的顺序进行的。坏处就太明显了，在网络环境无法保证畅通的情况，master存在着大大的失联风险，因此也给系统带来非常大的不确定性。系统的不确定性有多种可能，主机挂了，一部分从机挂了，或者两者都挂了；并且挂的时机不同，挂的顺序不同，还会造成不同的错误，所以，为了进一步提高网络的健壮性，我们可以考虑2PC。

##	Two phase commit (2PC)
2PC相比较于Primary/backup的1PC，其多出来的一步提供了可以回滚操作的能力。
>Note：2PC assumes that the data in stable storage at each node is never lost and that no node crashes forever. Data loss is still possible if the data in the stable storage is corrupted in a crash

2PA分为两步，第一步master给N个slave节点（N是slave节点的数量）发送更改的信息，slave节点接收到更新信息后，将相应的更改储存在**临时**的地方，并且返回一个表示“我ok“的信号，master收到N个”我ok“的信号后，会再次发送N个”走你“的信号，slave节点接收到”走你“的信号后，就真正把临时更改写入到内存中。一次2PC要进行3N个信息交互。2PC很容易阻塞，因为如果一台slave挂了，master需要等待这个slave重启并且发送”我ok“信号（这里假设崩溃的主机会自动恢复）。  
2PC基于的假设是数据存储是可靠的，节点不会永久故障，因此一旦这些假设不满足，数据还是有可能丢失的。在前一章CAP理论中，我们讲过2PC是一个CA系统，其考虑的失败模型中没有考虑network partitions，一旦发送网络分区，只能终止服务，人工接入，因此现在的系统一般都会考虑使用a partition tolerant consensus algorithm，这能够很好的处理网络分区。  

##	Partition tolerant consensus algorithms
分区容忍的算法比较有名的就是Paxos和Raft，在具体看算法之前，我们先回答第一个问题：  
What is a network partition?什么是网络分区
>A network partition is the failure of a network link to one or several nodes. The nodes themselves continue to stay active, and they may even be able to receive requests from clients on their side of the network partition

这里的network patition可以看成是两个节点相互失去连接。事实上，当两台节点相隔非常远的时候，我们很难确定失联是因为对面的节点挂了还是网络延时造成的，所以不妨把两个节点失联的情况统一看成是网络分区，网络分区的意思是两个节点处于两个不同的网络之中，无法进行交互，因为可能会出现信息冲突的情况。
![network patition](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter4-pat.png "network patition")
网络分区的一个特点是我们很难网络分区和节点故障区分开，一旦网络分区发生，系统中就会有多个部分都是出于active的状态，在Primary/backup中就会出现两个primary。因此，Partition tolerant consensus algorithms必须要解决的一个问题就是：during a network partition, only one partition of the system remains active

解决的方法主要有：
*	Majority decisions：在N个节点中，只有有N/2+1个还正常就能正常工作
*	Roles：有两种思路（all nodes may have the same responsibilities, or nodes may have separate, distinct roles.）通过选出一个master，能使系统变得更有效，最简单的好处就是：操作都经过master，就使得所有的操作都强制排序了。
*	Epochs：Epochs作用类似于逻辑时钟，能够使得不同节点对当前系统状态有个统一的认知

除了上面给出的方法外，还需要注意：
*	practical optimizations:
	*	avoiding repeated leader election via leadership leases (rather than heartbeats)【防止重复leader选举，手段是通过租期而不是心跳】
	*	avoiding repeated propose messages when in a stable state where the leader identity does not change【防止重复propose消息】
*	ensuring that followers and proposers do not lose items in stable storage and that results stored in stable storage are not subtly corrupted (e.g. disk corruption)【对于items要持久化存储防止丢失】
*	enabling cluster membership to change in a safe manner (e.g. base Paxos depends on the fact that majorities always intersect in one node, which does not hold if the membership can change arbitrarily)
*	procedures for bringing a new replica up to date in a safe and eﬃcient manner after a crash, disk loss or when a new node is provisioned
*	procedures for snapshotting and garbage collecting the data required to guarantee safety after some reasonable period (e.g. balancing storage requirements and fault tolerance requirements)

#	Paxos
paxos协议是分布式研究史上一朵璀璨的仙葩，一个现代各种分布式架构的基石，一种出了名的难以读懂的算法（基础是很好懂的，然而具体到工程领域就非常复杂了）。由于应用场景不同，paxos出现了很多变体，虽然2013年有大牛提出了更易于教学的raft，但是在很多领域，还是得用回复杂难懂但是有用的paxos协议（或其变体）。  
Paxos用于解决分布式系统中一致性问题。分布式一致性算法（Consensus Algorithm）是一个分布式计算领域的基础性问题，其最基本的功能是为了在多个进程之间对某个（某些）值达成一致（强一致）；简单来说就是确定一个值，一旦被写入就不可改变。paxos用来实现多节点写入来完成一件事情，例如mysql主从也是一种方案，但这种方案有个致命的缺陷，如果主库挂了会直接影响业务，导致业务不可写，从而影响整个系统的高可用性。paxos协议是只是一个协议，不是具体的一套解决方案。目的是解决多节点写入问题。paxos协议用来解决的问题可以用一句话来简化：
>将所有节点都写入同一个值，且被写入后不再更改。

## paxos的基本概念：
*	两个操作：
	*	Proposal Value：提议的值；【可以理解为某个节点将干的事情，比如说将变量name修改为handsome】
	*	Proposal Number：提议编号，可理解为提议版本号，要求不能冲突；【提议编号用于后续比较，比如说，我怎么知道哪个提议比较新？通过给每个提议加上一个编号就行了，这样接收到不同提议的节点就可以判断出1、它该不该接受这些提议（如果接收到的提议号都小于本地收藏的提议编号，就不接收）2、该接受哪个（接受比本地编号大的最大的那个提议，但是接受提议编号，不代表接受提议的值，具体看后面）
*	三个角色
	*	Proposer：提议发起者。Proposer 可以有多个，Proposer 提出议案（value）。所谓 value，可以是任何操作，比如“设置某个变量的值为value”。不同的 Proposer 可以提出不同的 value，例如某个Proposer 提议“将变量 X 设置为 1”，另一个 Proposer 提议“将变量 X 设置为 2”，但对同一轮 Paxos过程，最多只有一个 value 被批准。
	*	Acceptor：提议接受者；Acceptor 有 N 个，Proposer 提出的 value 必须获得超过半数(N/2+1)的 Acceptor批准后才能通过。Acceptor 之间完全对等独立。
	*	Learner：提议学习者。上面提到只要超过半数accpetor通过即可获得通过，那么learner角色的目的就是把通过的确定性取值同步给其他未确定的Acceptor。
proposer可以看成是接受了某个client的请求，然后将client的请求广播到其他节点，要求其他节点做出同样更新的节点a。acceptor就是那些应节点a要求进行更新的节点。learner的作用是将最后分布式系统达成的提议（因为可能同时存在多个提议（也就是说有多个信息需要修改，或者说有一个变量值在同一时刻收到了来自不同client的更改请求），所以需要通过竞争来确定一个提议）广播到所有的节点上。

## paxos的原则：
*	安全原则---保证不能做错的事
	*	 针对某个实例的表决只能有一个值被批准，不能出现一个被批准的值被另一个值覆盖的情况；(假设有一个值被多数Acceptor批准了，那么这个值就只能被学习)
	*	每个节点只能学习到已经被批准的值，不能学习没有被批准的值。
*	存活原则---只要有多数服务器存活并且彼此间可以通信，最终都要做到的下列事情：
	*	最终会批准某个被提议的值；
	*	一个值被批准了，其他服务器最终会学习到这个值。

##	paxos的流程：
一开始，所有acceptor的本地编号都是0，本地接受的更改是null（接受的更改对应于proposal value）。
*	1、准备阶段：
	*	第一阶段A：proposer选择一个提议编号n，向所有acceptor广播编号为n，更改为v的提议。
	*	第一阶段B：接收到提议编号n的节点（由于网络原因，不是所有节点都能接收到编号为n的提议），将n与自己本身保存的编号curN比较，此时又细分为两种情况：
		*	节点本身并没有接受任何更改，还是null，那么如果n>=curN，那么就将curN改成N，并且返回”我ok”；如果n<curN，就返回curN和“我不ok”
		*	节点本身已经接受了某个更改v，那么，如果n>curN，那么返回当前更改v，和当前编号curN,然后再将curN改成n，**注意了，这里并没有接受编号n的提议值，而是返回本节点已经接受了的提议值**；如果n=curN，那么就将本地的提议值改成接收到的提议值，并且返回”ok了“；如果n<curN，那还是什么都不做。
*	2、接受阶段：
	*	第二阶段A：proposer得到了来自acceptor的回应
		*	如果未超过半数acceptor响应，直接转为提议失败
		*	如果超过半数(N/2+1)acceptor响应，则进一步判断：
			*	1、如果所有accptor都未接受过值（都是null），那么向所有的acceptor发起自己的提议编号n和提议值v。**注意，是所有接收到的acceptor的回应都是null，此时不代表其他没响应的acceptor是null”
			*	2、如果有部分acceptor接受过值，那么从所有接受过的值中**选择对应提议编号最大**的值作为自己的提议值，提议编号仍未n，但此时proposer不能提议自己的值，而是使用acceptor中提议编号最大的值。
	*	第二阶段B：Acceptor接收到提议后，如果该提议编号不等于自身保存记录的编号，则不会更改本地的提议值，若是提议编号大于自己保存的编号，则更新自己的编号（**不会更新自己的提议值**），并返回自己现有的提议值；编号相等则写入本地。
	
##	paxos提议编号ID生成的算法：
在Google的Chubby论文中给出了这样一种方法：假设有n个proposer，每个编号为ir(0<=ir<n)，proposal编号的任何值s都应该大于它已知的最大值，并且满足：  

	s %n = ir    =>     s = m*n + ir  
	
proposer已知的最大值来自两部分：proposer自己对编号自增后的值和接收到acceptor的拒绝后所得到的值。  
例：  以3个proposer P1、P2、P3为例，开始m=0,编号分别为0，1，2。  
1） P1提交的时候发现了P2已经提交，P2编号为1 >P1的0，因此P1重新计算编号：new P1 = 1\*3+1 = 4；  
2） P3以编号2提交，发现小于P1的4，因此P3重新编号：new P3 = 1\*3+2 = 5。  

## 活锁
当某一proposer提交的proposal被拒绝时，可能是因为acceptor 承诺返回了更大编号的proposal，因此proposer提高编号继续提交。如果2个proposer都发现自己的编号过低转而提出更高编号的proposal，会导致死循环，这种情况也称为活锁。  
比如说当此时的 proposer1提案是3, proposer2提案是4, 但acceptor承诺的编号是5，那么此时proposer1,proposer2都将提高编号假设分别为6,7，并试图与accceptor连接，假设7被接受了，那么提案5和提案6就要重新编号提交，从而不断死循环。

## 例子
具体例子看[这里](https://blog.csdn.net/cnh294141800/article/details/53768464)

#	总结：
本章作者主要介绍了保证strong consistency的各种算法，以比较同步和异步复制开始，然后逐渐讨论随着考虑的错误变多算法需要怎么调整，下面是算法的一些关键点总结
*	Primary/Backup
	*	Single, static master
	*	Replicated log, slaves are not involved in executing operations
	*	No bounds on replication delay
	*	Not partition tolerant
	*	Manual/ad-hoc failover, not fault tolerant, "hot backup"
*	2PC
	*	Unanimous vote: commit or abort
	*	Static master
	*	2PC cannot survive simultaneous failure of the coordinator and a node during a commit
	*	Not partition tolerant, tail latency sensitive
*	Paxos
	*	Majority vote
	*	Dynamic master
	*	Robust to n/2-1 simultaneous failures as part of protocol
	*	Less sensitive to tail latency






