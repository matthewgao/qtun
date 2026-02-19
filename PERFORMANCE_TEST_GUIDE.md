# QTUN 性能测试指南

本指南提供了测试 QTUN 性能优化效果的方法和工具。

## 快速性能对比

### 1. 编译优化后的版本

```bash
# 使用优化编译选项
go build -o qtun-optimized -ldflags="-s -w" main.go

# 或使用 Makefile
make
```

### 2. 启动性能监控

优化后的版本已自动启用性能监控：

```bash
# pprof 端口: http://localhost:6060/debug/pprof/
# statsviz 端口: http://localhost:6060/debug/statsviz/
```

## 性能测试场景

### 场景 1: 吞吐量测试

测试大量数据传输的吞吐量。

**服务器端**:
```bash
sudo ./qtun-optimized qt \
  --key "test-key-12345" \
  --listen "0.0.0.0:8080" \
  --ip "10.4.4.1/24" \
  --server_mode \
  --log_level info
```

**客户端**:
```bash
sudo ./qtun-optimized qt \
  --key "test-key-12345" \
  --remote_addrs "SERVER_IP:8080" \
  --ip "10.4.4.2/24" \
  --log_level info
```

**测试命令**:
```bash
# 使用 iperf3 测试吞吐量
# 在服务器端 VPN IP 上启动 iperf3 server
iperf3 -s -B 10.4.4.1

# 在客户端通过 VPN 连接测试
iperf3 -c 10.4.4.1 -t 60 -P 10
```

### 场景 2: 延迟测试

测试 VPN 连接的延迟。

```bash
# 持续 ping 测试
ping -c 100 10.4.4.1

# 分析延迟分布
ping -c 1000 10.4.4.1 | awk -F'=' '/time=/{print $4}' | awk '{sum+=$1; sumsq+=$1*$1} END {print "Avg:", sum/NR, "ms, StdDev:", sqrt(sumsq/NR - (sum/NR)^2), "ms"}'
```

### 场景 3: 并发连接测试

测试多个并发连接的性能。

```bash
# 使用 ab (Apache Bench) 测试
ab -n 10000 -c 100 http://10.4.4.1/

# 或使用 wrk
wrk -t10 -c100 -d30s http://10.4.4.1/
```

### 场景 4: CPU 和内存使用

**实时监控**:
```bash
# 使用 top
top -p $(pgrep qtun)

# 或使用 htop
htop -p $(pgrep qtun)
```

**详细分析**:
```bash
# CPU profiling (30秒)
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# 内存分析
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Goroutine 分析
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
go tool pprof goroutine.prof
```

## 关键性能指标

### 1. 吞吐量指标

| 指标 | 测量方法 | 优化目标 |
|------|---------|---------|
| 数据传输速率 | iperf3 | +30-50% |
| 包转发率 (pps) | 自定义脚本 | +40-60% |

### 2. 延迟指标

| 指标 | 测量方法 | 优化目标 |
|------|---------|---------|
| 平均延迟 | ping | -20-40% |
| P99 延迟 | ping 统计 | -30-50% |

### 3. 资源使用指标

| 指标 | 测量方法 | 优化目标 |
|------|---------|---------|
| CPU 使用率 | top/htop | -10-20% |
| 内存分配 | pprof heap | -50-70% |
| Goroutine 数量 | pprof goroutine | 稳定 |
| GC 暂停时间 | statsviz | -40-60% |

## 性能对比脚本

创建 `benchmark.sh` 用于自动化测试：

```bash
#!/bin/bash

echo "=== QTUN Performance Benchmark ==="
echo "Date: $(date)"
echo ""

# 1. 基本信息
echo "## System Info"
echo "CPU: $(nproc) cores"
echo "Memory: $(free -h | awk '/^Mem:/{print $2}')"
echo ""

# 2. 进程信息
PID=$(pgrep qtun)
if [ -z "$PID" ]; then
    echo "Error: qtun is not running"
    exit 1
fi

echo "## QTUN Process"
echo "PID: $PID"
ps -p $PID -o %cpu,%mem,vsz,rss,cmd
echo ""

# 3. 网络延迟测试
echo "## Latency Test (100 pings)"
ping -c 100 -i 0.2 10.4.4.1 | tail -1
echo ""

# 4. 吞吐量测试
echo "## Throughput Test (10 seconds)"
iperf3 -c 10.4.4.1 -t 10 -P 4 | grep sender
echo ""

# 5. 内存使用
echo "## Memory Usage"
curl -s http://localhost:6060/debug/pprof/heap > /tmp/heap.prof
go tool pprof -top -sample_index=alloc_space /tmp/heap.prof 2>/dev/null | head -20
echo ""

# 6. Goroutine 数量
echo "## Goroutines"
curl -s http://localhost:6060/debug/pprof/goroutine | grep goroutine | head -1
echo ""

echo "=== Benchmark Complete ==="
```

## 优化验证清单

使用此清单验证各项优化是否生效：

### ✓ 对象池优化验证

```bash
# 检查内存分配次数（应显著减少）
curl -s http://localhost:6060/debug/pprof/allocs | go tool pprof -top -sample_index=alloc_objects -
```

预期：`getNonce`, `getBuffer`, `getEnvelope` 等函数应该频繁出现且分配次数少。

### ✓ 锁优化验证

```bash
# 检查锁争用（应显著减少）
curl -s http://localhost:6060/debug/pprof/mutex?debug=1 | head -50
```

预期：`FetchAndProcessTunPkt` 中的锁争用时间应明显降低。

### ✓ Channel 缓冲验证

查看日志，channel 阻塞应该减少，数据包丢失率降低。

### ✓ Worker 数量验证

```bash
# 检查日志中的 worker 数量
grep "Starting TUN packet workers" /var/log/qtun.log
```

预期：显示的 worker 数量应为 `CPU核心数 × 2`（最小4，最大32）。

### ✓ QUIC 配置验证

```bash
# 使用 tcpdump 检查 QUIC 连接参数
sudo tcpdump -i any -n 'udp port 8080' -X
```

预期：能看到更大的窗口大小公告。

## 长时间稳定性测试

运行以下脚本进行 24 小时稳定性测试：

```bash
#!/bin/bash

# 24小时压力测试
echo "Starting 24-hour stability test..."
START=$(date +%s)

while [ $(($(date +%s) - START)) -lt 86400 ]; do
    # 每分钟测试一次
    iperf3 -c 10.4.4.1 -t 10 -P 4 >> stability_test.log 2>&1
    
    # 记录内存使用
    ps -p $(pgrep qtun) -o rss= >> memory_usage.log
    
    # 记录 goroutine 数量
    curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | grep "goroutine profile:" >> goroutine_count.log
    
    sleep 60
done

echo "Stability test complete. Check logs for results."
```

## 结果分析

### 生成性能报告

```bash
#!/bin/bash

echo "# QTUN Performance Report"
echo "Generated: $(date)"
echo ""

echo "## Memory Stability"
echo "Memory usage trend (RSS in KB):"
cat memory_usage.log | awk '{sum+=$1; count++} END {print "Average:", sum/count, "Min:", min, "Max:", max}'
echo ""

echo "## Goroutine Stability"
echo "Goroutine count trend:"
grep "goroutine profile:" goroutine_count.log | awk '{print $3}' | awk '{sum+=$1; count++; if(NR==1){min=max=$1}} {if($1<min){min=$1}; if($1>max){max=$1}} END {print "Average:", sum/count, "Min:", min, "Max:", max}'
echo ""

echo "## Throughput Stability"
echo "Average throughput:"
grep "sender" stability_test.log | awk '{sum+=$(NF-2); count++} END {print sum/count, $(NF-1)}'
```

## 问题排查

### 如果性能未达预期

1. **检查 CPU 核心数**: 确认 worker 数量是否合理
   ```bash
   grep "num_workers" /var/log/qtun.log
   ```

2. **检查内存池**: 确认对象池正在使用
   ```bash
   curl http://localhost:6060/debug/pprof/allocs | go tool pprof -top -
   ```

3. **检查锁争用**: 确认锁优化生效
   ```bash
   curl http://localhost:6060/debug/pprof/mutex?debug=1
   ```

4. **检查网络**: 确认网络本身不是瓶颈
   ```bash
   iperf3 -c SERVER_IP -p DIFFERENT_PORT
   ```

## 推荐工具

- **iperf3**: 网络吞吐量测试
- **ping**: 延迟测试
- **wrk/ab**: HTTP 性能测试
- **pprof**: Go 性能分析
- **statsviz**: 实时可视化监控
- **htop**: 系统资源监控

---

测试愉快！如有问题请查看 PERFORMANCE_OPTIMIZATIONS.md 了解优化详情。

