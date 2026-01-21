# go_rtsp
golang rtsp for embedded

go run main.go //for test

./buildArm64.sh //build library for arm64 linux 

作为嵌入式库的使用说明：
首先拿到编译产物 bin/arm64/lib*

InitRTSPServer(8554);//初始化server，8554是监听的端口
AddStream(g_display_info[i].channel, strlen(g_display_info[i].channel));//传入stream地址，和地址长度，比如“1”

if (data && len > 0) {
            double current_ts = get_current_time();//拿到的是ms数据
            if(stream_start_time==0)
            {
                stream_start_time=current_ts;//首帧的时间戳
            }
            // 1. 计算距离开始经过了多少毫秒
            double diff_ms = current_ts - stream_start_time;
            // 2. 将毫秒转换为 90kHz 的时钟嘀嗒数 (1毫秒 = 90 ticks)
            uint32_t rtp_timestamp = (uint32_t)(diff_ms * 90);
                PushH265Frame(display_info->channel,
                              channel_len,
                              data,
                              len,
                              rtp_timestamp);
            }

Q&A:
Q.ffplay可以播放，但是vlc播放黑屏/卡顿
A.注意90khz的嘀嗒数据是否正常
