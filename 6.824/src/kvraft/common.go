package raftkv

const (
	OK       = "OK"
	ErrNoKey = "ErrNoKey"
)

type Err string

// Put or Append
type PutAppendArgs struct {
	Key   string
	Value string
	Op    string // "Put" or "Append"
	// You'll have to add definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	ClerkID 	int64 //发送消息的clerk编号
	CmdIndex 	int  //这个clerk发送的第几条信息
}

type PutAppendReply struct {
	WrongLeader bool
	Err         Err
}

type GetArgs struct {
	Key string
	// You'll have to add definitions here.
	ClerkID	int64
	CmdIndex 	int
}

type GetReply struct {
	WrongLeader bool   //true -> 对面不是leader
	Err         Err
	Value       string
}
