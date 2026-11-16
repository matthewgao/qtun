import Testing
@testable import ProxyBar

@Suite("模型校验与派生行为")
struct ModelTests {

    @Test func 解析host_port合法输入() {
        #expect(ProxyEndpoint.parse("127.0.0.1:7897") == ProxyEndpoint(host: "127.0.0.1", port: 7897))
        #expect(ProxyEndpoint.parse(" 127.0.0.1 : 7897 ") == ProxyEndpoint(host: "127.0.0.1", port: 7897))
        #expect(ProxyEndpoint.parse("10.5.5.2:2080") == ProxyEndpoint(host: "10.5.5.2", port: 2080))
    }

    @Test func 解析host_port非法输入为nil() {
        // 破坏点：端口越界/非数字/缺端口时仍返回端点。
        #expect(ProxyEndpoint.parse("host") == nil)
        #expect(ProxyEndpoint.parse("host:0") == nil)
        #expect(ProxyEndpoint.parse("host:65536") == nil)
        #expect(ProxyEndpoint.parse("host:abc") == nil)
        #expect(ProxyEndpoint.parse(":7897") == nil)
        #expect(ProxyEndpoint.parse("") == nil)
    }

    @Test func 端点校验端口边界与空host() {
        #expect(ProxyEndpoint(host: "h", port: 1).isValid)
        #expect(ProxyEndpoint(host: "h", port: 65535).isValid)
        #expect(!ProxyEndpoint(host: "h", port: 0).isValid)
        #expect(!ProxyEndpoint(host: "h", port: -1).isValid)
        #expect(!ProxyEndpoint(host: "  ", port: 80).isValid)
    }

    @Test func 配置校验_名称与端点与PAC() {
        var p = ProxyProfile(name: "ok", http: ProxyEndpoint(host: "h", port: 80))
        #expect(p.isValid)
        p.name = "   "
        #expect(!p.isValid)
        p.name = "ok"
        p.http = ProxyEndpoint(host: "h", port: 0)
        #expect(!p.isValid)
        p.http = nil
        p.pacURL = "notaurl"
        #expect(!p.isValid)
        p.pacURL = "ftp://x/pac"
        #expect(!p.isValid)
        p.pacURL = "http://127.0.0.1:6061/proxy.pac"
        #expect(p.isValid)
        p.pacURL = "file:///tmp/proxy.pac"
        #expect(p.isValid)
        p.pacURL = nil
        #expect(p.isValid)  // 全空配置合法（等价直连），由匹配器处理
    }

    @Test func 从系统状态生成配置_只取启用类型() {
        // 破坏点：把禁用类型的残留值也导入成新配置。
        let state = SystemProxyState(
            http: ProxyTypeState(enabled: false, host: "127.0.0.1", port: 7897),
            https: ProxyTypeState(enabled: true, host: "10.5.5.2", port: 2081),
            socks: ProxyTypeState(enabled: false, host: "", port: 0),
            pac: PACState(enabled: true, url: "http://127.0.0.1:6061/proxy.pac"),
            bypass: ["localhost", "<local>"]
        )
        let p = ProxyProfile.from(state: state, name: "导入")
        #expect(p.name == "导入")
        #expect(p.http == nil)
        #expect(p.https == ProxyEndpoint(host: "10.5.5.2", port: 2081))
        #expect(p.socks == nil)
        #expect(p.pacURL == "http://127.0.0.1:6061/proxy.pac")
        #expect(p.bypass == ["localhost", "<local>"])
    }

    @Test func isDirect_四类型全关才为直连() {
        // 破坏点：只看手动代理、漏掉 PAC。
        var s = SystemProxyState(
            http: ProxyTypeState(enabled: false, host: "127.0.0.1", port: 7897),
            https: ProxyTypeState(enabled: false, host: "", port: 0),
            socks: ProxyTypeState(enabled: false, host: "", port: 0),
            pac: PACState(enabled: false, url: nil),
            bypass: []
        )
        #expect(s.isDirect)  // 残留值不影响直连判定
        s.pac = PACState(enabled: true, url: "http://127.0.0.1:6061/proxy.pac")
        #expect(!s.isDirect)
        s.pac = PACState(enabled: false, url: nil)
        s.socks = ProxyTypeState(enabled: true, host: "10.5.5.2", port: 2080)
        #expect(!s.isDirect)
    }
}