package raftkv

import (
	"labrpc"
	"raft"
)
import "crypto/rand"
import "math/big"


type Clerk struct {
	servers []*labrpc.ClientEnd
	// You will have to modify this struct.
	leader 	int //记录最新的leader，方便下次通信
	me 	int64 //每个clerk独一无二的编号，调用nrand生成，5台机器中有两台机器相同的概率几乎可以忽略不计
	cmdIndex 	int //clerk给每个RPC调用编号
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// You'll have to add code here.
	ck.leader = 0 //默认一开始找第一个通信
	ck.me = nrand()
	ck.cmdIndex = 0
	raft.InfoKV.Printf("Client:%20v  | Create new clerk!\n", ck.me)
	return ck
}

//
// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
//
func (ck *Clerk) Get(key string) string {
	// You will have to modify this function.
	ck.cmdIndex++
	args := GetArgs{key, ck.me, ck.cmdIndex}
	leader := ck.leader
	raft.InfoKV.Printf("Client:%20v cmdIndex:%4d| Begin! Get:[%v] from server:%3d\n", ck.me, ck.cmdIndex, key, leader)


	for{
		reply := GetReply{}
		ok := ck.servers[leader].Call("KVServer.Get", &args, &reply)
		if ok && !reply.WrongLeader{
			ck.leader = leader
			//收到回复信息
			if reply.Value == ErrNoKey{
				//kv DB暂时没有这个key
				//raft.InfoKV.Printf("Client:20v cmdIndex:%4d | Get Failed! No such key\n", ck.me, ck.cmdIndex)
				return ""
			}
			raft.InfoKV.Printf("Client:%20v cmdIndex:%4d| Successful! Get:[%v] from server:%3d value:[%v]\n", ck.me, ck.cmdIndex, key, leader, reply.Value)
			return reply.Value
		}
		//对面不是leader Or 没收到回复
		leader = (leader + 1) % len(ck.servers)
		//raft.InfoKV.Printf("Client:%20v cmdIndex:%4d| Failed! Change server to %3d\n", ck.me, ck.cmdIndex, leader)
	}

}

//
// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
//
func (ck *Clerk) PutAppend(key string, value string, op string) {
	// You will have to modify this function.
	ck.cmdIndex++
	args := PutAppendArgs{key, value, op, ck.me, ck.cmdIndex}
	leader := ck.leader
	raft.InfoKV.Printf("Client:%20v cmdIndex:%4d| Begin! %6s key:[%s] value:[%s] to server:%3d\n", ck.me, ck.cmdIndex, op, key, value, leader)

	for{
		reply := PutAppendReply{}
		if ok := ck.servers[leader].Call("KVServer.PutAppend", &args, &reply); ok && !reply.WrongLeader && reply.Err == OK{
			raft.InfoKV.Printf("Client:%20v cmdIndex:%4d| Successful! %6s key:[%s] value:[%s] to server:%3d\n", ck.me, ck.cmdIndex, op, key, value, leader)
			ck.leader = leader
			return
		}
		//对面不是leader  Or 没收到回复
		leader = (leader + 1) % len(ck.servers)
		//raft.InfoKV.Printf("Client:%20v cmdIndex:%4d| Failed! Change server to %3d\n", ck.me, ck.cmdIndex, leader)
	}

}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}

