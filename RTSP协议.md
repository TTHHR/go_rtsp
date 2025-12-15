## #1. 协议概述 (Protocol Overview)

## 1.1 什么是 RTSP？

RTSP (Real-Time Streaming Protocol，实时流协议) 是 TCP/IP 协议体系中的一个应用层协议（定义于 RFC 2326）。打个最通俗的比方：RTSP 就是流媒体的“网络遥控器”。它的核心职责不是“搬运”视频数据，而是控制媒体流的播放状态。就像你手里的电视遥控器，它负责发送“播放”、“暂停”、“快进”、“停止”等指令，而电视屏幕上显示的画面（视频数据）通常是由另一套传输机制（如 RTP）来负责传送的。RTSP 的设计语法在很多方面与 HTTP 协议类似（基于文本，易于解析），但不同的是 RTSP 是**有状态（Stateful）**的协议。服务端需要维护客户端的会话状态（如：当前是处于“初始化”、“就绪”还是“播放中”）。

## 1.2 核心工作机制：控制与数据分离

RTSP 最显著的特征是采用双通道（或多通道）架构。控制通道 (Control Channel):通常使用 TCP 连接（默认端口 554）。用于传输 RTSP 指令（如 SETUP, PLAY, TEARDOWN）。保证命令的可靠到达。数据通道 (Data Channel):通常使用 UDP 或 TCP（取决于网络环境和协商结果）。用于传输实际的音视频载荷（通过 RTP 协议）。专注于数据的实时性。这种分离设计使得我们可以独立地控制媒体流，例如在不中断连接的情况下暂停视频，或者在同一个控制连接中点播另一段视频。

## 1.3 协议栈与关键资源 (Key Resources & Components)

在一次完整的 RTSP 流媒体会话中，RTSP 并不是单打独斗，它通常作为“指挥官”，协同以下几个关键资源（协议/组件）共同完成任务：


| 资源/组件 | 全称                         | 作用说明                                                                                                                                                                            |
| ----------- | ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| RTSP      | Real-Time Streaming Protocol | **指挥官** 。负责建立会话、协商传输方式、控制播放进度（Play/Pause/Stop）。                                                                                                          |
| SDP       | Session Description Protocol | **菜单/说明书** 。通常作为 RTSP 回复的一部分（在 `DESCRIBE` 阶段）。它告诉客户端：这个流里有什么（视频编码是 H.264 还是 H.265？音频是多少采样率？），以及连接所需的 IP 和端口信息。 |
| RTP       | Real-Time Transport Protocol | **搬运工** 。它负责把视频和音频数据切成小块（Packet），打上时间戳和序列号，通过网络发送给客户端。我们看到的画面数据都在 RTP 包里。                                                  |
| RTCP      | Real-Time Control Protocol   | **质检员** 。通常与 RTP 成对出现。它不传视频，而是周期性地发送统计报告（如丢包率、网络抖动、延迟）。RTSP 服务端可以根据 RTCP 的反馈来调整发送策略（例如网络太差时降低码率）。       |

## 1.4 协议解析实例
-----------------------
播放器请求查询服务器支持的功能  关键字 OPTIONS
-----------------------
<font color="#00ff00">[DEBUG] Received request:</font>
OPTIONS rtsp://172.28.197.26:8554/filetest RTSP/1.0
CSeq: 1
User-Agent: Lavf62.6.100

-----------------------
服务器返回消息说支持：OPTIONS, DESCRIBE, SETUP, TEARDOWN, PLAY, ANNOUNCE, RECORD
-----------------------
<font color="#309ae0ff">[DEBUG] Sending response:</font>
RTSP/1.0 200 OK
Public: OPTIONS, DESCRIBE, SETUP, TEARDOWN, PLAY, ANNOUNCE, RECORD
CSeq: 1
Server: THR's Server

-----------------------
播放器请求订阅 filetest 视频
-----------------------
<font color="#00ff00">[DEBUG] Received request:</font>
DESCRIBE rtsp://172.28.197.26:8554/filetest RTSP/1.0
Accept: application/sdp
CSeq: 2
User-Agent: Lavf62.6.100

-----------------------
服务器返回SDP消息，视频流为h265，streamid为f6b171c4c4071749e2ec0b9c435f2a30
-----------------------
<font color="#309ae0ff">[DEBUG] Sending response:</font>
RTSP/1.0 200 OK
CSeq: 2
Content-Base: rtsp://172.28.197.26:8554/filetest/
Server: THR's Server
Content-Type: application/sdp
Content-Length: 170

v=0
o=- 0 0 IN IP4 0.0.0.0
s=H265 Video Stream
c=IN IP4 0.0.0.0
t=0 0
m=video 30000 RTP/AVP 96
a=rtpmap:96 H265/90000
a=control:streamid=f6b171c4c4071749e2ec0b9c435f2a30

-----------------------
播放器请求初始化filetest,并附带了streamid，因为可能有多个rtsp客户端，服务端需要区分一下，
并且要求使用UDP方法，客户端开始监听16408/16409端口，一个是RTP一个是RTCP
-----------------------
<font color="#00ff00">[DEBUG] Received request:</font>
SETUP rtsp://172.28.197.26:8554/filetest/streamid=f6b171c4c4071749e2ec0b9c435f2a30 RTSP/1.0
Transport: RTP/AVP/UDP;unicast;client_port=16408-16409
CSeq: 3
User-Agent: Lavf62.6.100

-----------------------
服务器返回好的，UDP发送
-----------------------
<font color="#309ae0ff">[DEBUG] Sending response:</font>
RTSP/1.0 200 OK
Session: f6b171c4c4071749e2ec0b9c435f2a30;timeout=60
Transport: RTP/AVP/UDP;unicast;client_port=16408-16409;server_port=30002-30003
Server: THR's Server
CSeq: 3


-----------------------
播放器请求 PLAY ,并附带了session id
-----------------------
<font color="#00ff00">[DEBUG] Received request:</font>
PLAY rtsp://172.28.197.26:8554/filetest/ RTSP/1.0
Range: npt=0.000-
CSeq: 4
User-Agent: Lavf62.6.100
Session: f6b171c4c4071749e2ec0b9c435f2a30

-----------------------
服务器回复 好的
-----------------------
<font color="#309ae0ff">[DEBUG] Sending response:</font>
RTSP/1.0 200 OK
Range: npt=0.000-
CSeq: 4
Session: f6b171c4c4071749e2ec0b9c435f2a30
Server: THR's Server

