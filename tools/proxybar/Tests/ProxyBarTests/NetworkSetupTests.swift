import Testing
@testable import ProxyBar

// 夹具全部来自本机实测的 networksetup 输出（macOS 26.7.2）。
private let webProxyDisabledFixture = """
Enabled: No
Server: 127.0.0.1
Port: 7897
Authenticated Proxy Enabled: 0
"""

private let webProxyEnabledFixture = """
Enabled: Yes
Server: 10.5.5.2
Port: 2081
Authenticated Proxy Enabled: 0
"""

private let webProxyEmptyFixture = """
Enabled: No
Server:\u{20}
Port: 0
Authenticated Proxy Enabled: 0
"""

private let pacEnabledFixture = """
URL: http://127.0.0.1:6061/proxy.pac
Enabled: Yes
"""

private let pacNullFixture = """
URL: (null)
Enabled: No
"""

@Suite("NetworkSetup 输出解析")
struct NetworkSetupParserTests {

    @Test func 解析启用的代理输出() {
        let s = NetworkSetup.parseTypeState(webProxyEnabledFixture)
        #expect(s == ProxyTypeState(enabled: true, host: "10.5.5.2", port: 2081))
    }

    @Test func 解析禁用的代理输出_保留残留值() {
        // 破坏点：若解析时把禁用状态的 host/port 清空，原始值信息丢失。
        let s = NetworkSetup.parseTypeState(webProxyDisabledFixture)
        #expect(s == ProxyTypeState(enabled: false, host: "127.0.0.1", port: 7897))
    }

    @Test func 解析空值输出_iBridge实测() {
        let s = NetworkSetup.parseTypeState(webProxyEmptyFixture)
        #expect(s == ProxyTypeState(enabled: false, host: "", port: 0))
    }

    @Test func 解析容忍CRLF与多余空白() {
        let crlf = "Enabled: Yes\r\nServer:  10.5.5.2 \r\nPort: 2081\r\nAuthenticated Proxy Enabled: 0\r\n"
        let s = NetworkSetup.parseTypeState(crlf)
        #expect(s == ProxyTypeState(enabled: true, host: "10.5.5.2", port: 2081))
    }

    @Test func 解析启用的PAC输出() {
        let s = NetworkSetup.parsePACState(pacEnabledFixture)
        #expect(s == PACState(enabled: true, url: "http://127.0.0.1:6061/proxy.pac"))
    }

    @Test func 解析PAC的null为nil() {
        // 破坏点：把字符串 "(null)" 当成真实 URL。
        let s = NetworkSetup.parsePACState(pacNullFixture)
        #expect(s == PACState(enabled: false, url: nil))
    }

    @Test func 解析绕过域名列表_保留local标记() {
        let out = "127.0.0.1\nlocalhost\n*.local\n<local>\n"
        #expect(NetworkSetup.parseBypass(out) == ["127.0.0.1", "localhost", "*.local", "<local>"])
    }

    @Test func 解析绕过域名空列表提示语() {
        // 破坏点：把提示句子本身当成一条域名。
        let out = "There aren't any bypass domains on this network service."
        #expect(NetworkSetup.parseBypass(out) == [])
    }

    @Test func 解析绕过域名空输出() {
        #expect(NetworkSetup.parseBypass("") == [])
        #expect(NetworkSetup.parseBypass("  \n\n") == [])
    }
}

@Suite("NetworkSetup 命令构造")
struct NetworkSetupBuilderTests {

    private func clashProfile(bypass: [String] = ["localhost"]) -> ProxyProfile {
        ProxyProfile(
            name: "Clash",
            http: ProxyEndpoint(host: "127.0.0.1", port: 7897),
            https: ProxyEndpoint(host: "127.0.0.1", port: 7897),
            socks: ProxyEndpoint(host: "127.0.0.1", port: 7897),
            pacURL: nil,
            bypass: bypass
        )
    }

    @Test func Clash三端点配置_必须关闭PAC() {
        // 破坏点：漏掉 -setautoproxystate off，旧 PAC 仍生效，流量继续走 PAC。
        let cmds = NetworkSetup.applyCommands(profile: clashProfile(), service: "Wi-Fi")
        #expect(cmds == [
            ["-setwebproxy", "Wi-Fi", "127.0.0.1", "7897", "off"],
            ["-setsecurewebproxy", "Wi-Fi", "127.0.0.1", "7897", "off"],
            ["-setsocksfirewallproxy", "Wi-Fi", "127.0.0.1", "7897", "off"],
            ["-setautoproxystate", "Wi-Fi", "off"],
            ["-setproxybypassdomains", "Wi-Fi", "localhost"],
        ])
    }

    @Test func PAC_only配置_关闭手动代理_空bypass传Empty() {
        let p = ProxyProfile(
            name: "qtun PAC",
            pacURL: "http://127.0.0.1:6061/proxy.pac",
            bypass: []
        )
        let cmds = NetworkSetup.applyCommands(profile: p, service: "Wi-Fi")
        #expect(cmds == [
            ["-setwebproxystate", "Wi-Fi", "off"],
            ["-setsecurewebproxystate", "Wi-Fi", "off"],
            ["-setsocksfirewallproxystate", "Wi-Fi", "off"],
            ["-setautoproxyurl", "Wi-Fi", "http://127.0.0.1:6061/proxy.pac"],
            ["-setproxybypassdomains", "Wi-Fi", "Empty"],
        ])
    }

    @Test func 直连_恰好四条off_不动bypass() {
        // 破坏点：直连时多写 bypass 或漏关某一类型。
        let cmds = NetworkSetup.applyCommands(profile: nil, service: "Wi-Fi")
        #expect(cmds == [
            ["-setwebproxystate", "Wi-Fi", "off"],
            ["-setsecurewebproxystate", "Wi-Fi", "off"],
            ["-setsocksfirewallproxystate", "Wi-Fi", "off"],
            ["-setautoproxystate", "Wi-Fi", "off"],
        ])
    }

    @Test func 带空格的服务名保持单个参数() {
        // 破坏点：把服务名按空格拆分传给 Process。
        let cmds = NetworkSetup.applyCommands(profile: nil, service: "Thunderbolt Bridge")
        #expect(cmds.allSatisfy { $0[1] == "Thunderbolt Bridge" })
        #expect(cmds.count == 4)
    }

    @Test func 读取命令映射() {
        #expect(NetworkSetup.getArgs(.http, service: "Wi-Fi") == ["-getwebproxy", "Wi-Fi"])
        #expect(NetworkSetup.getArgs(.https, service: "Wi-Fi") == ["-getsecurewebproxy", "Wi-Fi"])
        #expect(NetworkSetup.getArgs(.socks, service: "Wi-Fi") == ["-getsocksfirewallproxy", "Wi-Fi"])
        #expect(NetworkSetup.getArgs(.pac, service: "Wi-Fi") == ["-getautoproxyurl", "Wi-Fi"])
        #expect(NetworkSetup.getBypassArgs(service: "Wi-Fi") == ["-getproxybypassdomains", "Wi-Fi"])
    }
}