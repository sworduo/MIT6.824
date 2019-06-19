#!/bin/bash
for((i=1; i<=10; i++))
do
	go test -race -run=TestFigure8Unreliable2C > $"out_"$i$".txt"  && rm infoRaft.log warnRaft.log infoKV.log
done
