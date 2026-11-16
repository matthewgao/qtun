import Testing
@testable import ProxyBar

private let clash = ProxyProfile(
    name: "Clash",
    http: ProxyEndpoint(host: "127.0.0.1", port: 7897),
    https: ProxyEndpoint(host: "127.0.0.1", port: 7897),
    socks: ProxyEndpoint(host: "127.0.0.1", port: 7897),
    pacURL: nil,
    bypass: ["localhost"]
)

@Suite("ProxyApplier 命令编排")
struct ProxyApplierTests {

    @Test func 按序执行完整命令序列() async {
        // 破坏点：跳过命令、乱序、服务间交叉。
        let runner = FakeRunner()
        let applier = ProxyApplier(runner: runner)
        let failures = await applier.apply(profile: clash, services: ["Wi-Fi", "Ethernet"])
        #expect(failures.isEmpty)
        let executed = runner.executed
        #expect(executed.count == 10)
        #expect(executed[0] == ["-setwebproxy", "Wi-Fi", "127.0.0.1", "7897", "off"])
        #expect(executed[3] == ["-setautoproxystate", "Wi-Fi", "off"])
        #expect(executed[4] == ["-setproxybypassdomains", "Wi-Fi", "localhost"])
        #expect(executed[5] == ["-setwebproxy", "Ethernet", "127.0.0.1", "7897", "off"])
        #expect(executed[9] == ["-setproxybypassdomains", "Ethernet", "localhost"])
    }

    @Test func 单条失败被收集_其余继续执行() async {
        // 破坏点：遇错即停，后面的类型没关掉。
        let runner = FakeRunner()
        runner.failures = ["-setwebproxy Wi-Fi 127.0.0.1 7897 off"]
        let applier = ProxyApplier(runner: runner)
        let failures = await applier.apply(profile: clash, services: ["Wi-Fi"])
        #expect(failures.count == 1)
        #expect(failures[0].service == "Wi-Fi")
        #expect(failures[0].exitCode == 1)
        #expect(failures[0].message.contains("boom"))
        #expect(runner.executed.count == 5)
    }

    @Test func 直连执行四条off() async {
        let runner = FakeRunner()
        let applier = ProxyApplier(runner: runner)
        _ = await applier.apply(profile: nil, services: ["Wi-Fi"])
        #expect(runner.executed == [
            ["-setwebproxystate", "Wi-Fi", "off"],
            ["-setsecurewebproxystate", "Wi-Fi", "off"],
            ["-setsocksfirewallproxystate", "Wi-Fi", "off"],
            ["-setautoproxystate", "Wi-Fi", "off"],
        ])
    }
}

@Suite("ProxyStateReader 状态读取")
struct ProxyStateReaderTests {

    private func wiredRunner() -> FakeRunner {
        let r = FakeRunner()
        r.outputs = [
            "-getwebproxy Wi-Fi": "Enabled: Yes\nServer: 127.0.0.1\nPort: 7897\nAuthenticated Proxy Enabled: 0\n",
            "-getsecurewebproxy Wi-Fi": "Enabled: Yes\nServer: 127.0.0.1\nPort: 7897\nAuthenticated Proxy Enabled: 0\n",
            "-getsocksfirewallproxy Wi-Fi": "Enabled: No\nServer: 10.5.5.2\nPort: 2080\nAuthenticated Proxy Enabled: 0\n",
            "-getautoproxyurl Wi-Fi": "URL: (null)\nEnabled: No\n",
            "-getproxybypassdomains Wi-Fi": "127.0.0.1\nlocalhost\n<local>\n",
        ]
        return r
    }

    @Test func 读取并组装完整状态() async {
        // 破坏点：get 命令与类型映射错位、bypass 没读。
        let reader = ProxyStateReader(runner: wiredRunner())
        let s = await reader.read(service: "Wi-Fi")
        #expect(s == SystemProxyState(
            http: ProxyTypeState(enabled: true, host: "127.0.0.1", port: 7897),
            https: ProxyTypeState(enabled: true, host: "127.0.0.1", port: 7897),
            socks: ProxyTypeState(enabled: false, host: "10.5.5.2", port: 2080),
            pac: PACState(enabled: false, url: nil),
            bypass: ["127.0.0.1", "localhost", "<local>"]
        ))
    }

    @Test func 多服务并发读取() async {
        let runner = wiredRunner()
        runner.outputs["-getwebproxy Ethernet"] = "Enabled: No\nServer:\nPort: 0\nAuthenticated Proxy Enabled: 0\n"
        let reader = ProxyStateReader(runner: runner)
        let states = await reader.read(services: ["Wi-Fi", "Ethernet"])
        #expect(states.count == 2)
        #expect(states["Wi-Fi"]?.http.enabled == true)
        #expect(states["Ethernet"]?.isDirect == true)
    }

    @Test func 读取失败降级为关闭态() async {
        // 破坏点：get 失败时崩溃或把垃圾输出当有效值。
        let runner = FakeRunner()
        runner.failures = ["-getwebproxy Wi-Fi"]
        let reader = ProxyStateReader(runner: runner)
        let s = await reader.read(service: "Wi-Fi")
        #expect(s.http == ProxyTypeState(enabled: false, host: "", port: 0))
        #expect(s.isDirect)
    }
}