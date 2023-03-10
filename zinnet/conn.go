package zinnet

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/helenvivi/zinx/utils"
	"github.com/helenvivi/zinx/zinterface"
)

// 定义链接的结构
type Connection struct {
	//conn 属于 Server
	ConnServer zinterface.IServer

	//socket 套接字
	conn *net.TCPConn

	//conn 的 ID
	ConnID uint32

	//链接状态
	IsCloesd bool

	//告知当前链接状态的channel、reader --> writer
	ExitChan chan bool

	//无缓冲channel、用在writer reader发送信息
	MessChan chan []byte

	//消息管理模块
	MessageHandler zinterface.IMsgHandler

	//连接属性集合
	property map[string]interface{}

	//给链接属性加锁
	propertyLock sync.RWMutex
}

// read模块
func (c *Connection) StartRead() {
	fmt.Println("[StartRead goroutine is running]")
	defer fmt.Println("[Writer Conn Exit!]", "Connid ", c.ConnID, "C Addr ", c.RemoteAddr().String())
	defer c.Stop()
	for {
		//建立一个dp拆包对象
		dp := NewData()

		//建立二进制msg head
		DataHead := make([]byte, dp.GetHeadlen())
		//读取客户端conn的msg Head
		_, err := io.ReadFull(c.GetTcpConn(), DataHead)
		if err != nil {
			fmt.Println("read msg head error", err)
			break
		}
		//使用dp拆包
		msghead, err := dp.UnPackMsg(DataHead)
		if err != nil {
			fmt.Println("Server msg Unpack error", err)
			break
		}
		// 转换对象，将接口转换为实例对象
		msg := msghead.(*Message)
		//判断msg有无数据、若有则进行数据包的拆包
		if msg.GetMessageLen() > 0 {
			//开辟 Data空间
			msg.Date = make([]byte, msg.GetMessageLen())
			//从流中读取剩余的data
			_, err := io.ReadFull(c.GetTcpConn(), msg.Date)
			if err != nil {
				fmt.Println("read msg data error", err)
				break
			}
		}
		//执行注册的路由方法
		//go c.MessageHandler.DoMsgHandler(NewRequest(c, msg))
		if utils.Globa.MaxPoolSize > 0 {
			//开辟pool
			c.MessageHandler.SendMsgTaskQueue(NewRequest(c, msg))
		} else {
			//不开辟pool
			go c.MessageHandler.DoMsgHandler(NewRequest(c, msg))
		}
	}
}

// write 模块
func (c *Connection) StartWrite() {
	fmt.Println("StartWrite goroutine is running")
	defer fmt.Println("[Writer Conn Exit!]", "Connid ", c.ConnID, "C Addr ", c.RemoteAddr().String())

	//堵塞等待msgchan
	for {
		select {
		case date := <-c.MessChan:
			//msgchan到达
			_, err := c.conn.Write(date)
			if err != nil {
				fmt.Println("Send data error!")
				return
			}
			//exitchan到达
		case <-c.ExitChan:
			return
		}

	}
}

// start conn、启动链接、让连接开始工作
func (c *Connection) Start() {
	fmt.Println("conn start()...connid = ", c.ConnID)

	//启动当前链接的读
	go c.StartRead()
	//启动当前链接的写
	go c.StartWrite()

	//执行服务端对应应答
	c.ConnServer.GetHook().CallOnConnStart(c)
}

// stop conn、停止链接、结束当前连接的工作
func (c *Connection) Stop() {
	fmt.Println("[Conn stop()...]connid = ", c.ConnID)
	if c.IsCloesd {
		return
	}
	c.IsCloesd = true
	//执行服务端对应应答
	c.ConnServer.GetHook().CallOnConnStop(c)

	//关闭socket链接
	c.conn.Close()
	//告知writer exitchan关闭
	c.ExitChan <- true

	//删除当前链接
	c.ConnServer.GetConnMgr().DeleteConn(c)

	//关闭channel、回收资源
	close(c.ExitChan)
	close(c.MessChan)

}

// 获取当前链接的绑定socket 套接字
func (c *Connection) GetTcpConn() *net.TCPConn {
	return c.conn
}

// 获取当前conn 的ID
func (c *Connection) GetConnID() uint32 {
	return c.ConnID
}

// 获取远程客户端的tcp状态、IP、Port
func (c *Connection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// 发送数据
func (c *Connection) Send(msgId uint32, data []byte) error {
	if c.IsCloesd {
		return errors.New("conn is closed when send msg")
	}
	//定义封包对象
	dp := NewData()
	//封装
	buf, err := dp.PackMsg(NewMsg(msgId, data))
	if err != nil {
		return errors.New("pack error msg")
	}
	//发送数据给管道
	c.MessChan <- buf
	return nil
}

// 实例化对象conn、初始化模块的方法,向外暴露接口
func NewConn(s zinterface.IServer, conn *net.TCPConn, connId uint32, mh zinterface.IMsgHandler) zinterface.Iconn {
	c := &Connection{
		ConnServer: s,
		conn:       conn,
		ConnID:     connId,
		// handleAPI: api,
		IsCloesd:       false,
		ExitChan:       make(chan bool, 1),
		MessChan:       make(chan []byte),
		MessageHandler: mh,
	}
	c.ConnServer.GetConnMgr().AddteConn(c)
	return c
}

// 设置连接属性
func (c *Connection) SetProperty(key string, value interface{}) {
	c.propertyLock.Lock()
	defer c.propertyLock.Unlock()

	c.property[key] = value
}

// Get 属性
func (c *Connection) GetProperty(key string) (interface{}, error) {
	c.propertyLock.Lock()
	defer c.propertyLock.Unlock()

	v, ok := c.property[key]
	if !ok {
		fmt.Println("Not Fount property[key]")
		return nil, errors.New("this key Not in conn")
	}
	return v, nil
}

// 移除连接属性
func (c *Connection) RemoveProperty(key string) {
	c.propertyLock.Lock()
	defer c.propertyLock.Unlock()

	delete(c.property, key)
}
