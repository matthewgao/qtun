import Testing
import Foundation
@testable import ProxyBar

private let off = ProxyTypeState(enabled: false, host: "", port: 0)
private let pacOff = PACState(enabled: false, url: nil)

private func clashProfile(id: UUID = UUID(), name: String = "Clash") -> ProxyProfile {
    ProxyProfile(
        id: id, name: name,
        http: ProxyEndpoint(host: "127.0.0.1", port: 7897),
        https: ProxyEndpoint(host: "127.0.0.1", port: 7897),
        socks: ProxyEndpoint(host: "127.0.0.1", port: 7897),
        pacURL: nil, bypass: ["localhost"]
    )
}

private func clashState(host: String = "127.0.0.1", port: Int = 7897) -> SystemProxyState {
    let on = ProxyTypeState(enabled: true, host: host, port: port)
    return SystemProxyState(http: on, https: on, socks: on, pac: pacOff, bypass: [])
}

private let directState = SystemProxyState(http: off, https: off, socks: off, pac: pacOff, bypass: [])

private func pacState(url: String = "http://127.0.0.1:6061/proxy.pac") -> SystemProxyState {
    // 本机实测形态：PAC 开着，手动代理关着但残留 Clash 值。
    SystemProxyState(
        http: ProxyTypeState(enabled: false, host: "127.0.0.1", port: 7897),
        https: off, socks: off,
        pac: PACState(enabled: true, url: url),
        bypass: ["<local>"]
    )
}

private let pacProfile = ProxyProfile(
    name: "qtun PAC",
    pacURL: "http://127.0.0.1:6061/proxy.pac",
    bypass: []
)

@Suite("ProfileMatcher 单服务匹配")
struct ProfileMatcherMatchTests {

    @Test func 完全一致则匹配() {
        #expect(ProfileMatcher.matches(clashProfile(), clashState()))
    }

    @Test func host忽略大小写与空白() {
        // 破坏点：逐字节比较 host。
        var s = clashState()
        let weird = ProxyTypeState(enabled: true, host: " LOCALHOST ", port: 7897)
        s.http = weird; s.https = weird; s.socks = weird
        let p = ProxyProfile(
            name: "x",
            http: ProxyEndpoint(host: "localhost", port: 7897),
            https: ProxyEndpoint(host: "localhost", port: 7897),
            socks: ProxyEndpoint(host: "localhost", port: 7897)
        )
        #expect(ProfileMatcher.matches(p, s))
    }

    @Test func 端口不一致则不匹配() {
        #expect(!ProfileMatcher.matches(clashProfile(), clashState(port: 7890)))
    }

    @Test func 配置未声明的类型必须关闭() {
        // 破坏点：PAC-only 配置匹配上了开着 SOCKS 的状态。
        #expect(!ProfileMatcher.matches(pacProfile, clashState()))
    }

    @Test func 禁用状态的残留值被忽略() {
        // 破坏点：拿禁用类型的残留 host/port 参与比较。
        #expect(ProfileMatcher.matches(pacProfile, pacState()))
    }

    @Test func PAC_URL不一致则不匹配() {
        #expect(!ProfileMatcher.matches(pacProfile, pacState(url: "http://127.0.0.1:9999/other.pac")))
    }

    @Test func PAC_URL两边trim后相等即匹配() {
        var p = pacProfile
        p.pacURL = "  http://127.0.0.1:6061/proxy.pac  "
        #expect(ProfileMatcher.matches(p, pacState()))
    }

    @Test func bypass差异不影响匹配_v1语义() {
        var p = clashProfile()
        p.bypass = ["a", "b"]
        var s = clashState()
        s.bypass = ["c"]
        #expect(ProfileMatcher.matches(p, s))
    }
}

@Suite("ProfileMatcher 跨服务选择")
struct ProfileMatcherSelectTests {

    @Test func 全部直连选direct() {
        let sel = ProfileMatcher.select(states: ["Wi-Fi": directState], profiles: [clashProfile()])
        #expect(sel == .direct)
    }

    @Test func 直连优先于全空配置() {
        // 破坏点：全 nil 配置匹配直连态，抢走了 .direct。
        let empty = ProxyProfile(name: "empty")
        let sel = ProfileMatcher.select(states: ["Wi-Fi": directState], profiles: [empty])
        #expect(sel == .direct)
    }

    @Test func 全部服务匹配同一配置则选中() {
        let p = clashProfile()
        let sel = ProfileMatcher.select(
            states: ["Wi-Fi": clashState(), "Ethernet": clashState()],
            profiles: [p]
        )
        #expect(sel == .profile(p.id))
    }

    @Test func 服务间状态不一致则为custom() {
        // qtun 只改 Wi-Fi 的场景：混合态必须诚实显示自定义。
        let sel = ProfileMatcher.select(
            states: ["Wi-Fi": clashState(), "Ethernet": directState],
            profiles: [clashProfile()]
        )
        #expect(sel == .custom)
    }

    @Test func 无配置匹配则为custom() {
        let sel = ProfileMatcher.select(states: ["Wi-Fi": clashState(port: 1234)], profiles: [clashProfile()])
        #expect(sel == .custom)
    }

    @Test func 重复配置取列表序第一个() {
        let first = clashProfile(name: "first")
        let second = clashProfile(name: "second")
        let sel = ProfileMatcher.select(states: ["Wi-Fi": clashState()], profiles: [first, second])
        #expect(sel == .profile(first.id))
    }

    @Test func 空服务集合为custom() {
        // 破坏点：空集合 allSatisfy 恒真被误判为 direct。
        #expect(ProfileMatcher.select(states: [:], profiles: []) == .custom)
    }
}