#	Distributed systems for fun and profit
推荐一个非常好的分布式系统[入门博客](http://book.mixu.net/distsys/)，能了解分布式的相关知识，阅读完这个博客之后，可以去上MIT6.824看paper做project了。  
作者的[gayhub](https://github.com/mixu/)  

#	总览：
大部分分布式编程主要是解决如何又快又好的使用多台机器来完成一个任务的问题。  
*	[Basic](http://book.mixu.net/distsys/intro.html "chapter1"):第一章主要是介绍一些有关分布式的一些术语和概念。
*	[Up and down the level of abstraction](http://book.mixu.net/distsys/abstractions.html "chapter2"):第二章主要是深入介绍具体的抽象模型理论，并且讲述一些impossibility results。
*	[Time and order](http://book.mixu.net/distsys/time.html "chapter3"):第三章主要讨论time, order and clock以及三者的多种组合使用。这是非常重要的一个章节，某种程度上来讲，你对这三者的领悟决定了你所设计的分布式系统的性能。
*	[Replication:preventing divergence](http://book.mixu.net/distsys/replication.html "chapter4"):第四章主要讲述replication的问题，以及两种主流的实现replication的方法
*	[Replication:accepting divergence](http://book.mixu.net/distsys/eventual.html "chapter5"):第五章讨论弱一致性保证下的replicatoin。
*	[Appendix](http://book.mixu.net/distsys/appendix.html "chapter6"):一些paper。(ps:我觉得这章可以不看了，直接上mit6.824那里看paper就行)

本人还是刚入门的萌新，很多分布式系统的概念和知识都不会，所以如果有错漏的地方，请大家不吝指出，一笑而过，而且这里只是写了一点小总结，推荐大家去看原文。  20190225一