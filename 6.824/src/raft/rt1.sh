#!/bin/bash
for((i=1; i<=30; i++))
do
	rm $"out_"$i$".txt" 
done
 rm raftInfo.log raftWarn.log shardInfo.log kvInfo.log
