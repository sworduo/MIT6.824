#	[Time and order](http://book.mixu.net/distsys/time.html)
	author:sworduo	date:Feb 27, Wed, 2019
[参考](https://www.jianshu.com/p/f0993c83cdf5):袖珍分布式系统（三）

![cute](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter3-head.jpg "cute")

首先来看文章提出的第一个问题：
>what is order and why is is important?

order可以说是贯穿整个分布式系统的一个基石问题，之前说到，一致性问题是其他高阶算法的基础，同样的，这里的order问题则是一致性问题的基础。还记得分布式系统设计的初衷是什么吗？
>the art of solving the same problem that you can solve on a single computer using multiple computers.

我们希望用分布式算法统筹的多机系统表现得和单机系统一样，这就引出了一个非常重要的问题：order。  

##	order的重要性
如果多个节点有了一个统一的order，那么我们可以很方便的给不同节点上的任务标定一个适用于全局的标记，有了这个全局有效的标记，就能控制任务按照我们预设的顺序执行，这样一来多机就真的和单机没什么区别了。这个时候可以很方便的把大部分单机上执行的程序移植到多机系统上，然而这是一个美好的幻想。

##	实现order的难点
我们可以很轻易的在单机上控制任务执行的次序，只要按照任务进来的顺序给定相应的编号，那么我们就能控制任务执行的顺序，甚至能预测任务完成的时间等等。然而同样的问题扩大到分布式系统时就变得十分棘手，因为程序实际的执行顺序你是无法预测的。因为物理等其他原因，每个节点的时钟并不是完全一致的，这种微小偏差对于人类来说几乎可以忽略不计，但是对于ms甚至是ps精度的计算机而言，一点细小的偏差将会导致非常大的错误，日积月累下来将会影响整个系统的运行。    
解决节点之间的同步有两种思路：
*	使用复杂的容错的技术来实现所有节点的时钟同步，然后根据时间戳生成全局有效的先后标识作为任务执行的凭证，从而得到一个全局的total order。这个思路的难点在于，如何在地里位置相隔甚远的情况下，实现一个多机共享的，ms、微秒甚至ps是误差精度的时钟。
*	通过communication system，给每个操作编号，从而得到一个顺序。然而这个思路的难点在于，分布式系统中网络通信是不可靠的，您不可能完全确定另一个节点的状态。

#	Total and partial order
在分布式环境中一种常见的状态是partial order。其含义是集合中的任意两个元素不一定可以进行比较。比如说假设A比B高，A也比C高，然而你无法通过比较来确定B和C谁高。针对分布式系统的场景，因为时钟很难真正达到一致，所以分布式网络中每个节点都有其局部时间，所以，假如节点A同时受到节点B和C的消息时，A很难确定B和C的消息究竟谁快谁慢，有可能因为误差累积的原因，B的记录时间大于C，然而实际时间却是小于C，这种情况下，时间的先后只能在单机上比较。  
那么我们能否在分布式网络中实现一个total order，使得不同节点共享同一个准确的次序呢，很难，因为：
>communication is expensive, and time synchronization is diﬃcult and fragile

#	Time
从前面的讲述中，其实我们可以意识到，次序是和时间紧密相连的，在单机系统中，因为只存在一个时间，所以次序可以用时间的大小的来进行表示和比较。  
那么，什么是时间？
>Time is a source of order - it allows us to deﬁne the order of operations- which coincidentally also has an interpretation that people can understand (a second, a minute, a day and so on).

时间有时候就像一个计数器，只不过时间这个计数器比较重要，我们用这个计数器产生的数值来定义整个人类的最重要的概念：时间。

>Timestamps really are a shorthand value for representing the state of the world from the start of the universe to the current moment - if something occurred at a particular timestamp, then it was potentially inﬂuenced by everything that happened before it.

什么是时间戳（Timestamps），Timestamps定义了世界从初始到现在的状态，如果某件事发生在一个特定的时间点上，是之前影响产生的结果。时间戳的概念可以泛化到因果时钟上，因果时钟认为此时此刻发生的事情和之前发生的事情存在因果关系，所以可以通过当前时刻逆推出之前时刻和此刻有关系的事件，而不仅仅是简单的认为之前发生的所有事情都和此时的事情有关。  
这个概念基于的前提是：所有的时间都以相同的速率前行着，time and timestampes在程序中应用时，通常有三个解释：
*	order
*	duration
*	interpretation
前面提到的"time is a source of order"的含义是：
*	we can attach timestamps to unordered events to order them【通过给事件安排一个时间戳，从而给事件排序】
*	we can use timestamps to enforce a speciﬁc ordering of operations or the delivery of messages (for example, by delaying an operation if it arrives out of order)【我们可以通过时间戳给操作重新排序】
*	we can use the value of a timestamp to determine whether something happened chronologically before something else【通过时间戳知道哪个事件发生在前】
>Interpretation - time as a universally comparable value.
时间戳的绝对值解释为日期（date），这是人们非常容易理解并且加以运用的概念。
>Duration - durations measured in time have some relation to the real world.
像算法一般只关心duration，通过duration来判断延迟，一个好的算法都希望能有低延时。

#	Does time progress at the same rate everywhere?
分布式网络中，各个节点的时间可以以同样的速率前进吗？有三个常见的回答：
*	"Global clock": yes
*	"Local clock": no, but
*	"No clock": no!
以上三种看待时间的角度和之前提到的三种系统模型息息相关：
*	the synchronous system model has a global clock,
*	the partially synchronous model has a local clock, and
*	in the asynchronous system model one cannot use clocks at all
下面逐一来解释：

##	Time with a "global-clock" assumption
![global](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter3-global-clock.png "cute")

当我们认可全局时钟的概念时，等同于我们接受分布式网络各个节点共享同一个非常精确的，几乎没有偏差的时钟的假设，我们从任何时刻任何节点所看到的时间应该基本等同于其他地方其他节点此时此刻的时间，这也是平时生活中我们习以为常的时钟，同样的，正如上面提到一样，我们可以以较大的偏差来接受这个时钟，而分布式网络则以非常严苛的偏差来接受这个时钟。  
有了global-clock，那么我们可以通过timestamp来生成一个total order，一定程度上可以把此时的分布式系统看成是单机网络，然而维持较大范围内的时钟同步是一件非常困难的事情，我们只能做到一定范围内的同步。（我觉得一般当问题有两种解决思路时，最佳的解决方法就是两种都用，比如小范围内用时钟同步，大范围内用后面提到的vector clock方法，这样可能效果是最好的，没验证，只是章口就莱。）  
目前，忽略时钟不同步问题做出来的系统有：
*	Facebook's Cassandra:通过时间戳来解决冲突。
*	Google's Spanner:时间戳+偏差范围来定义顺序。

##	Time with a "Local-cloak" assumption
![local](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter3-local-clock.png "cute")

>events on each system are ordered but events cannot be ordered across systems by only using a clock.
此时每个节点有各自的时间，因而节点内部的任务可以通过时间戳来排序，但是不同节点上的时间戳不能比较。

##	Time with a "No-clock" assumption
不在使用时间戳，而是使用counter，通过传递消息来交换counter，从而定义不同机器之间的事件的前后顺序，由于没有时钟的存在，所以无法设定超时等概念。比较有名的论文就是：time, clocks and the ordering of events。

#	How is time used in a distributed system?
时间的好处是：
*	Time can deﬁne order across a system (without communication)【时间可以不通过通信而在整个分布式网络中维持一个统一的时钟】
*	Time can deﬁne boundary conditions for algorithms【可以定义一些边界条件，比如失败的条件等等】
在分布式系统中，定义时间的顺序非常重要，因为：
*	where correctness depends on (agreement on) correct event ordering, for example serializability in a distributed database【正确性依赖于事件的顺序】
*	order can be used as a tie breaker when resource contention occurs, for example if there are two orders for a widget, fulﬁll the ﬁrst and cancel the second one【当发生资源争用的时候可以用来做裁决】
如果我们有全局时钟，就可以不通过通信来确定事物的顺序了，不幸的是，我们一般没有，所以只能通过通信来确定顺序。  
此外，时间还可以用来区分high latency和server or network link is down.而区分两者的算法就是failure detectors.

##	Vector clocks(time for causal order)
时间之所以重要，是因为我们需要时间来确定事物发生的顺序，然而，我们真正想要的自始至终都是顺序，而不是时间，那么，我们能不能以其他设计为基础来定义事物的顺序呢？还真是有，Lamport clocks和vector clocks是两种替代物理时钟的方法。这两种方法的核心都在于维护一个本地的计数器，然后通过通信来更新计数器，以此来为本地的任务进行标记，而这种标记是可以在全局进行比较的。   

###	Lamport clock
Lamport clock里每个节点只维护自身的计数器，其更新规则：
*	Whenever a process does work, increment the counter【工作时计数器加一】
*	Whenever a process sends a message, include the counter【发送信息时，附上当前记数器额值】
*	When a message is received, set the counter to max(local_counter, received_counter) + 1【接收到信息时，更新自身的计数器】
然而这种方法有些缺点。比如，两件任务同时在A和B上独立运行，这时候他们之间就很难判断优先级了。再比如A同时收到B和C的信息，若B只与A交互，而C与成百上千台机器交互，那么C的数值将会非常大，这时候如果B和C的任务同时到来，那么A基本上只会执行C的任务，而不会去执行B的任务，即便可能B的任务更加新，然而由于其计数器非常小而可能被认为是旧的任务。（这里我也不是很懂，也是章口就莱）

###	Vector clock
vector clock里每个节点维护和它直接通信过的、或者是它知道的其他节点的计数值，比如A知道BCD，那么A和E第一次通信之后，E不仅知道了和它直接通信的A，还知道了A已知的BCD。其更新规则如下：
*	Whenever a process does work, increment the logical clock value of the node in the vector【只更新自己的值】
*	Whenever a process sends a message, include the full vector of logical clocks【发送整个向量】
*	When a message is received:
	*	update each element in the vector to be max(local, received)
	*	increment the logical clock value representing the current node in the vector
	
![time](https://raw.githubusercontent.com/sworduo/MIT6.824/master/brief%20introduction/pic/chapter3-time.png "cute")

#	Failure detectors(time for cutoff)
在分布式环境中，我们怎么知道一个节点已经不可用了呢？我们可以等待一段时间，如果这段时间超过预设的时间阈值，那么就认为对面宕机了。  
那么，这个时间阈值应该怎么设置？  
首先这不应该是一个固定的值，因为网络环境和节点之间的延迟千变万化。所以阈值的选择非常灵活。
>A failure detector is a way to abstract away the exact timing assumptions. Failure detectors are implemented using heartbeat messages and timers. Processes exchange heartbeat messages. If a message response is not received before the timeout occurs, then the process suspects the other process.

failure detectors有两个重要的属性，每个属性又有两个衍生的概念：
*	Strong completeness:Every crashed process is eventually suspected by every correct process.
*	Weak completeness:Every crashed process is eventually suspected by some correct process.
*	Strong accuracy:No correct process is suspected ever.
*	Weak accuracy:Some correct process is never suspected.
可以很方便的基于weak completeness来实现strong completeness，只要利用广播就行，所以一般而言，分布式编程的重点在于accuracy.  
failure detectors是一个非常重要的工具，有了它，我们可以判断一个远程节点是出于高延时状态还是宕机状态。宕机状态的远程节点我们可以不再关注，但是，若远程主机仅仅是处于高延迟状态，那么我们就必须同步，或者是做一些设计中应该要做的事情。  


