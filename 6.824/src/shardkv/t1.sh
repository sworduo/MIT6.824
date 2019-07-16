#!/bin/bash
for((i=1; i<=15; i++))
do
	go test -run=TestJoinLeave > $"out_"$i$".txt" && rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
done
