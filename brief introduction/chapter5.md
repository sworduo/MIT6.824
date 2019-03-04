#	[Replication: weak consistency model protocols](http://book.mixu.net/distsys/eventual.html)
	author:sworduo	date:Mar 4, Mon, 2019
![cute](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter5-head.jpg "cute")

在分布式系统中，要维持“多机表现的像单机”一样是非常困难的，原因在于维护一致性的成本很高，特别是在物理位置相隔很远的两台服务器之间维持一致性难度更高。一致性的难点在于：我们无法保证网络通信是一定可用的，由于网络分区（两台主机失联）的可能性非常大，很容易造成信息冲突；另一方面，任务的顺序也很难保持一致，若选择不通过通信的方式来维护一个全局一致的序号，那么由于时钟的偏差，一段时间后两台主机的时间就会出现较大的差别，造成后来的事件反而排在前面的情况，若选择网络通信的方式来维持一个序号，则要面临网络分区的风险。处理一致性一个较好的方法就是paxos，不同于要求全部主机达成一致，其只要(N/2+1)台主机达成一致(N是节点数量），这时候，因为任意两个大小为(N/2+1)的集合一定存在交集，所以只要(N/2+1)台节点达成一致，那么慢慢的，所有主机都会达成一致，当然paxos也会出现永远无法达成一致的情况（活锁），不过我们暂时不考虑这个。  
由于维护一致性的成本很高，因此我们需要考虑一下，真的所有任务都需要维持强一致性吗？对于某些可以不需要维持强一致性的任务来说（比如不需要考虑信息到达的次序，只要保证信息都到达了，我们就能得到正确的结果），此时维护强一致性是不必要并且是降低系统可用性的，所以对于这一类任务，我们可以使用一些新的方法。  

#	Reconciling different operation orders
在分布式网络中，常常出现次序不统一的情况，比如有123三种信息在分布式网络中传输，AB主机接受的信息次序可能是这样的：

	[A]->1 2 3
	[B]->2 1 3
	
这种不一致性很可能会造成毁灭性的后果，比如，假设我们传输的信息不是数字，而是字符串"hello""world""!"

	[A]->"hello" "world" "!"
	[B]->"world" "!" "hello"
	
当出现这种情况时，可怕的不是顺序不一致，而是怕出现字符串两种不同的顺序排列组合都成立，但是意思截然不同的情况，这是最致命的。比如：

	[A]->“你”“爱”“我”“不“
	[B]->”我“”不“”爱“”你“
	
这时候AB接收到的字符串排列后都是有意义的，但是意思截然不同。

#	Partial quorums
之前我们提到的同步读写模型，写的慢，读的快，有保障；异步读写模型，写的快，读的也快，然而正确性没有保障。那我们能不能设计一种新的读写方法，在同/异步之间取得一个速度和质量的平衡呢？partial quorums就是一种类似的方法：  
*	the user can choose some number W-of-N nodes required for a write to succeed; and
*	the user can specify the number of nodes (R-of-N) to be contacted during a read.
简单来说，每次写的时候将信息同步到W（W<=N)台主机上,读的时候读取R（R<=N）台主机的信息，然后进行比较/合并，根据标记选择最新的返回。只要保证W+R大于N，那么就能获得较强的一致性保证。假设主机数量N=3，那么有：
*	R=1,W=N,读的快，写的慢，有保障，此时就是同步模型。
*	R=N,W=1,读的慢，写的快，不太保险，因为唯一存储信息的那台节点可能会挂掉。
*	R=N/2,W=N/2+1,两者之间取得平衡。读的比2快，写的比2快，并且读的结果正确性也挺高的。 

那么R+W>N能否保证强一致性呢？  

	答案是：不
	
这是因为系统很难保证N台主机是不变的，具体来说，系统可以保证一共有N台主机，但是不能保证不同时刻的N台主机都是一样的，因为网络分区、宕机等情况的出现，那些无法联络的主机会被集群删去，然后添加新的、不相关的但是可以用的主机，此时新的主机并没有之前保存的信息。此时读的R台主机可能是由存储旧信息的主机+新加入的主机构成，因此读会出错。

#	Conflict detection and read repair
当我读集群中的R台主机时，假如R台主机之间的信息有冲突，我如何决定应该返回哪一个信息：
*	no metadata:系统没有引入任何额外的用于判断顺序的标记，此时返回最后一个到达的主机的信息，比如读ABC三台主机的信息，假设A的信息最后到达，就返回A的信息，虽然有可能C的才是最新的。
*	Timestamps：用每个信息的时间戳来判断消息的时间顺序。由于时间同步的问题，两台主机之间的时间信息并不一定是可比较的，比如两台主机都是从00:00开始计数，都是通过”人呼吸一次“所花费的时间+1s，然而，很显然两个人的呼吸时间并不完全一致，所以这两台主机”+1s“所用的时间也是不同的，造成两台主机时间不同步，此时两台主机的信息合并时，我们很难通过时间戳来判断信息真实发生的时间的先后顺序。
*	version numbers:？？？好像还不错。
*	vertor clocks:使用这个可以判断并发和过时的数据。然而对于一些并发的数据，我们需要人工去确定使用哪一个，因为我们不知道哪个才是最新的。

#	CRDTs: Convergent replicated data types
有一些数据类型是不在乎数据到达的先后次序的，只要求这些数据都到达就可以，不要求到达的顺序。换言之，只要不同的主机接收到相同的数据，那么他们就能得到相同的结果。这些信息一般具有以下这些特点：
*	Associative (a+(b+c)=(a+b)+c), so that grouping doesn't matter
*	Commutative (a+b=b+a), so that order of application doesn't matter
*	Idempotent (a+a=a), so that duplication does not matter

举个例子，假如此时节点的任务是计算最大值，显然不管数据到达的顺序如何，最后得到的最大值也是一样的。其他例子：
*	Counters
	*	Grow-only counter (merge = max(values); payload = single integer)
	*	Positive-negative counter (consists of two grow counters, one for increments and another for decrements)
*	Registers
	*	Last Write Wins -register (timestamps or version numbers; merge = max(ts); payload = blob)
	*	Multi-valued -register (vector clocks; merge = take both)
*	Sets
	*	Grow-only set (merge = union(items); payload = set; no removal)
	*	Two-phase set (consists of two sets, one for adding, and another for removing; elements can be added once and removed once)
	*	Unique set (an optimized version of the two-phase set)
	*	Last write wins set (merge = max(ts); payload = set)
	*	Positive-negative set (consists of one PN-counter per set item)
	*	Observed-remove set
*	Graphs and text sequences (see the paper)