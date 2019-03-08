#	[Up and down the level of abstraction](http://book.mixu.net/distsys/abstractions.html)
	author:Sworduo	date:Feb 26, Tue, 2019 
[参考1](https://cloud.tencent.com/developer/article/1193906):袖珍分布式系统（二）   
[参考2](https://www.ruanyifeng.com/blog/2018/07/cap.html):CAP定理的含义  
![cute](https://github.com/sworduo/Course/blob/master/pic/MIT6.824/introduction/chapter2-head.jpg "cute")

　　分布式编程主要是解决由于分布式而带来的一系列问题。我们希望把分布式系统看成是具有超大规模处理能力的单机系统，然而这种高层次的抽象会失去许多底层的细节，虽然便于理解，但是加大了编程的难度。所以分布式的抽象主要是在“实现”和“理解”之间取得一个平衡。抽象层次越高，理解起来越简单，实现起来越困难，事实上，这是一个贯穿于整个计算机系统各个子领域的权衡问题，没有通用的最优解，只有针对具体的合适的解决方法。我们的目标旨在寻找到一个足够好的抽象模型，尽可能让编程变得简单的同时容易让人可以理解。  
那么，我们上面说要在“实现”和“理解”之间寻找到一个合适的抽象，那如何定义什么是合适的抽象？
>What do we mean when say X is more abstract than Y? First, that X does not introduce anything new or fundamentally diﬀerent from Y. In fact, X may remove some aspects of Y or present them in a way that makes them more manageable. Second, that X is in some sense easier to grasp than Y, assuming that the things that X removed from Y are not important to the matter at hand.

　　简单来说，就是用尽可能少的假设来描述清楚一个东西。你用的假设越少，你基于这个假设所涉及的系统普适性更高，能处理的场景也就越多，然而随之而来的，就是编程难度的提高。  
抽象能帮助我们从纷繁复杂的细节中提炼出问题的关键，当我们找到问题的症结时，就能设计出相应的解决方案，所以很多时候难点并不在于解决困难，而是发现问题，甚至是意识到原来这是一个问题。  
　　这同样引出一个问题，描述清楚一个东西所需要的最少假设是多少？最简单的方法是列出所有可能的假设，然后一样一样的排除，思考当我排除掉这个假设时，剩余的假设能否清楚的描述一个东西？如果能，就舍去这个假设，如果不能，就留下这个假设。然而“最少的假设”本身就是不存在的，针对每个问题都有不同的“最少假设”，甚至针对不同时期的同一个问题，随着产品的扩大，其所需要的“最小假设”也是不断变化的，前期被舍去的假设后面可能要被重新引入，这是一个动态变化的过程。然而不管怎样变化，核心要义还是用最少的假设去完成我们想要的功能。  
针对分布式，根据能保证系统正常运行的同时，所需要的最少假设所演化出来的设计就是我们常说的系统模型。  

#	A system model
分布式系统最大的属性就是分布式，一个分布式系统中的程序至少需要含有以下这些特点：
*	run concurrently on independent nodes【任务可以在各个独立节点上并发执行】
*	are connected by a network that may introduce nondeterminism and message loss【节点之间通过网络互连，这可能会造成信息丢失和不确定性】
*	and have no shared memory or shared clock【不共享内存和时钟】
具体的解释是：
*	each node executes a program concurrently【每个节点都并发执行】
*	knowledge is local: nodes have fast access only to their local state, and any information about global state is potentially out of date 【每个节点只知道自己节点上的信息】
*	nodes can fail and recover from failure independently 【每个节点失败和恢复都是独立的】
*	messages can be delayed or lost (independent of node failure; it is not easy to distinguish network failure and node failure) 【通信是不可靠的】
*	and clocks are not synchronized across nodes (local timestamps do not correspond to the global real time order, which cannot be easily observed)【时钟不同步】
系统模型的定义：
> System model：a set of assumptions about the environment and facilities on which a distributed system is implemented

简单的说，系统模式就是实现分布式系统的环境和工具所依赖的一系列假设。系统模型中还定义了关于environment and facilities的假设，这些假设包括：
*	what capabilities the nodes have and how they may fail 【每个节点能力和失败方式】
*	how communication links operate and how they may fail and 【节点间通信方式和失败方式】
*	properties of the overall system, such as assumptions about time and order【整个系统属性：如时序】
健壮系统：基于最少的假设来设计的系统，系统在于普适性强，缺点在于难以理解。  
下面具体介绍nodes的属性以及links and time and order。

##	Nodes in our system model
节点需要提供计算和存储能力，其拥有以下这些特点：
*	the ability to execute a program【执行程序】
*	the ability to store data into volatile memory (which can be lost upon failure) and into stable state (which can be read after a failure)【在不稳定的内存中存储信息的能力，以及恢复正常的能力】
*	a clock (which may or may not be assumed to be accurate)【时钟】
有许多可能的failure model，一种是crash-recovery failure model，指的是系统只能因为崩溃而拒绝提供服务，并且能在崩溃后自动恢复。另一种是Byzantine fault tolerance，这是现实生活中几乎不会遇到的模型，因为其允许出现随机的错误，显然这种系统非常作，很难伺候。  

##	Communication links in our system model
分布式系统中最难处理的假设就是通信假设，我们在分布式系统中，一个系统很难知道另一个系统的情况，因为任何的通信都是不可靠的，信息都无法交流，还怎么知道别人的情况，因此分布式系统中，能依赖的只有节点本身的信息。

##	Timing / ordering assumptions
在分布式系统中我们必须认识到：每个node看到的世界都是不同的，这个不同来自于一个事实：信息的传输需要时间。对于同一件事情，每个节点看到这个事情的时间都是不一样的，因此每个节点看到的世界，其时间点都是不同的。  
有两个关于时间的主要的模型：
*	 Synchronous system: model Processes execute in lock-step; there is a known upper bound on message transmission delay; each process has an accurate clock
*	 Asynchronous system:model No timing assumptions - e.g. processes execute at independent rates; there is no bound on message transmission delay; useful clocks do not exist

##	The consensus problem
下面对网络是否分区包含在错误模型中和网络传输是同步还是异步模型两个条件的讨论
*	whether or not network partitions are included in the failure model, and【网络分区是否考虑在模型中】
*	synchronous vs. asynchronous timing assumptions【同/异步】
首先介绍一下什么是一致性模型：
*	Agreement: Every correct process must agree on the same value.【节点内容一致】
*	Integrity: Every correct process decides at most one value, and if it decides some value, then it must have been proposed by some process.【不太懂说啥，意思可能是值和机器直接有对应关系】
*	Termination: All processes eventually reach a decision.【所有节点最终会达成一致】
*	Validity: If all correct processes propose the same value V, then all correct processes decide V.【所有节点观点一致时，其所作出的决定就是有效的】
一致性问题是分布式系统里最核心的问题，解决了这个问题，我们就不用关注各个节点数据之间可能出现的不一致和分歧，这也是解决后续许多高级问题的基石。  

#	Two impossibility results
什么是impossibility results:
>A proof of impossibility, also known as negative proof, proof of an impossibility theorem, or negative result, is a proof demonstrating that a particular problem cannot be solved, or cannot be solved in general. Often proofs of impossibility have put to rest decades or centuries of work attempting to find a solution. To prove that something is impossible is usually much harder than the opposite task; it is necessary to develop a theory. Impossibility theorems are usually expressible as universal propositions in logic (see universal quantification).

当确定了不可能结果之后，我们就不用再在这个方向上白费力气，这也是一种排除法，可以指导我们解决问题的方向。  
在分布式系统中存在两个最重要的不能结果，FLP和CAP,FLP主要用于学术研究，这里不讲，下面着重介绍CAP。

##	The CAP theorem
CAP的含义：
*	Consistency: all nodes see the same data at the same time.（一致性）
*	Availability: node failures do not prevent survivors from continuing to operate.（可用性）
*	Partition tolerance: the system continues to operate despite message loss due to network and/or node failure（分区容忍性）
具体来说，分区容错的意思是，区间通信可能会失败，比如一台机器在中国，一台机器在美国，他们之间可能无法通信，实际编程时需要考虑到两台机器之间无法通信/通信失败的情况。  
一致性是指某台机器将某个对象的值修改后，其他人在其他机器再次访问同一个对象时，都应该看到新值而不是旧值。  
可用性是指只要收到用户的请求，服务器就必须返回。  
>一致性和可用性的矛盾：一致性和可用性很难同时成立，因为存在通信失败的可能。  
假如有两台机器A和B，当你在A修改了对象C的值，这时候如果强调一致性，那么A机器需要锁定B机器的读和写，在B将对象C的值修改之前，B机器不能进行任何其他的读和写，若在这期间有用户对服务器发出对对象C的请求，将不会得到任何答复，这样可用性的不满足了。若要满足可用性，那么B机器的读写就不能被锁定，这样由于延时等原因，一致性就不能满足了，所以二者不可兼得。  

事实上，CAP三个特性只有2个能同时满足，因此会出现3种系统：
*	CA (consistency + availability). Examples include full strict quorum protocols, such as two-phase commit.
*	CP (consistency + partition tolerance). Examples include majority quorum protocols in which minority partitions are unavailable such as Paxos.
*	AP (availability + partition tolerance). Examples include protocols using conﬂict resolution, such as Dynamo.
CA和CP系统都提供了强一致性模型，不同是CA不可以容忍网络分区，而CP在2f+1个节点中，可以容忍f个节点失败，原因很简单：
*	A CA system does not distinguish between node failures and network failures, and hence must stop accepting writes everywhere to avoid introducing divergence (multiple copies). It cannot tell whether a remote node is down, or whether just the network connection is down: so the only safe thing is to stop accepting writes.【不能区分网络分区和节点失败，因此必须停止写入避免引入不一致】
*	A CP system prevents divergence (e.g. maintains single-copy consistency) by forcing asymmetric behavior on the two sides of the partition. It only keeps the majority partition around, and requires the minority partition to become unavailable (e.g. stop accepting writes), which retains a degree of availability (the majority partition) and still ensures single-copy consistency.【即使网络分区了，大多数节点的一方还是能够提供服务】
CP系统因为将网络分区考虑到了failure model中，因此能够通过类似Paxos, Raft 的协议来区分a majority partition and a minority partition  
CA则由于没有考虑网络分区的情况，因此无法知道一个节点不响应式因为节点收不到消息还是节点失败了，因此只能够通过停止服务来防止出现数据一致，在CA中由于不能保证网络可靠性，因此通过使用two-phase commit algorithm来保证数据一致性。  
从CAP理论中，我们可以得到4个结论：
*	First, that many system designs used in early distributed relational database systems did not take into account partition tolerance (e.g. they were CA designs). Partition tolerance is an important property for modern systems, since network partitions become much more likely if the system is geographically distributed (as many large systems are).【早期系统大多没有考虑P，因此是CA系统，但是现代系统，特别是出现异地多主后，必须考虑分区了】
*	Second, that there is a tension between strong consistency and high availability during network partitions. The CAP theorem is an illustration of the tradeoﬀs that occur between strong guarantees and distributed computation.【P既然无法避免，我们只能在C和A之间做选择，有时候我们可以通过降低数据的一致性模型，不再追求强一致，从而达到"CAP"】
*	Third, that there is a tension between strong consistency and performance in normal operation.【当一个操作涉及的消息数和节点的数少的时候，延迟自然就低，但是这也意味着有些节点不会被经常访问，意味着数据会是旧数据】
*	Fourth - and somewhat indirectly - that if we do not want to give up availability during a network partition, then we need to explore whether consistency models other than strong consistency are workable for our purposes.【有时候3选2可能是误解，我们如果将自己不限制在强一致性模型，我们会有更多的选择】
一致性并不是一个一成不变的定义，根据具体场景不同，可以设计出不同的“一致性”。我们要记住：  
> ACID consistency != CAP consistency != Oatmeal consistency

一致性模型的概念是：
>Consistency model:a contract between programmer and system, wherein the system guarantees that if the programmer follows some speciﬁc rules, the results of operations on the data store will be predictable

一致性模型是编程者和系统之间的契约，只要编程者按照某种规则，那计算机的操作结果就是可预测的。  
下面介绍一些一致性模型：

#	Strong consistency vs. other consistency models
*	Strong consistency models (capable of maintaining a single copy) 
	*	Linearizable consistency
	*	Sequential consistency
*	Weak consistency models (not strong) 
	*	Client-centric consistency models
	*	Causal consistency: strongest model available
	*	Eventual consistency models
一致性模型可以分为两大类：强一致和弱一致。强一致模型给编程者提供的是一个和单机系统一样的模型，而弱一致，则让编程者清楚的意识要是在分布式环境下编程，而不是单机环境。

##	Strong consistency models
强一致性模型可以再细分为两大类：
*	Linearizable consistency: Under linearizable consistency, all operations appear to have executed atomically in an order that is consistent with the global real-time ordering of operations. (Herlihy & Wing, 1991)
*	Sequential consistency: Under sequential consistency, all operations appear to have executed atomically in some order that is consistent with the order seen at individual nodes and that is equal at all nodes. (Lamport, 1979)
两者的最大不同是：linearizable consistency要求操作的结果要和操作实际执行的顺序一致，而Sequential consistency则允许操作实际发生的顺序和操作产生结果的顺序不同，只要每个节点看到的顺序是一样的就行。两者之间的差别基本上可以忽略。

##	Client-centric consistency models
该一致性模型主要是为了解决下面的情况：客户端进行了某个操作，同时也看到了最新的结果，但是由于网络中断，重新连接到server，此时不能因为重新连接而看到一个旧的结果。

##	Eventual consistency
最终一致性我们需要知道两点：
*	First, how long is "eventually"? It would be useful to have a strict lower bound, or at least some idea of how long it typically takes for the system to converge to the same value【最终一致，这个最终是多久？我们需要有个下限，或者至少是一个平均值】
*	Second, how do the replicas agree on a value? 【多个副本怎么达成一致？】
因此，在谈论最终一致的时候，我们需要知道这可能是："eventually last-writer-wins, and read-the-latest-observed-value in the meantime"
