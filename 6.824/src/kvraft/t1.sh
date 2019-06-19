#!/bin/bash
for((i=1; i<=10; i++))
do
	go test > $"out_"$i$".txt" && rm infoRaft.log warnRaft.log infoKV.log
done
