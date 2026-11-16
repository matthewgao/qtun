import Testing
@testable import ProxyBar

// 本机实测输出。
private let listAllFixture = """
An asterisk (*) denotes that a network service is disabled.
iBridge
Wi-Fi
Thunderbolt Bridge
"""

private let orderFixture = """
(1) iBridge
(Hardware Port: iBridge, Device: en5)

(2) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)

(3) Thunderbolt Bridge
(Hardware Port: Thunderbolt Bridge, Device: bridge0)
"""

@Suite("ServiceEnumerator 解析与目标规则")
struct ServiceEnumeratorTests {

    @Test func 合并两个列表_丢弃说明头() {
        // 破坏点：把 header 行当成服务；hardware/device  merge 错位。
        let services = ServiceEnumerator.parse(listAll: listAllFixture, order: orderFixture)
        #expect(services == [
            NetworkService(name: "iBridge", hardwarePort: "iBridge", device: "en5", enabled: true),
            NetworkService(name: "Wi-Fi", hardwarePort: "Wi-Fi", device: "en0", enabled: true),
            NetworkService(name: "Thunderbolt Bridge", hardwarePort: "Thunderbolt Bridge", device: "bridge0", enabled: true),
        ])
    }

    @Test func 星号标记禁用() {
        let listAll = """
An asterisk (*) denotes that a network service is disabled.
Wi-Fi
*Ethernet
"""
        let order = """
(1) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)

(*) (2) Ethernet
(Hardware Port: Ethernet, Device: en6)
"""
        let services = ServiceEnumerator.parse(listAll: listAll, order: order)
        #expect(services.count == 2)
        #expect(services[0].enabled)
        #expect(!services[1].enabled)
        #expect(services[1].device == "en6")
    }

    @Test func 默认目标规则_排除iBridge与bridge设备与禁用服务() {
        // 破坏点：iBridge 实测报 Enabled，只靠 enabled 过滤会把它选中。
        let services = ServiceEnumerator.parse(listAll: listAllFixture, order: orderFixture)
        #expect(ServiceEnumerator.isDefaultTarget(services[0]) == false)  // iBridge
        #expect(ServiceEnumerator.isDefaultTarget(services[1]) == true)   // Wi-Fi
        #expect(ServiceEnumerator.isDefaultTarget(services[2]) == false)  // bridge0
        let disabled = NetworkService(name: "Ethernet", hardwarePort: "Ethernet", device: "en6", enabled: false)
        #expect(!ServiceEnumerator.isDefaultTarget(disabled))
    }

    @Test func 用户覆盖可增删目标() {
        let services = ServiceEnumerator.parse(listAll: listAllFixture, order: orderFixture)
        let included = ServiceEnumerator.targets(services, overrides: ["Thunderbolt Bridge": true])
        #expect(included.map(\.name) == ["Wi-Fi", "Thunderbolt Bridge"])
        let excluded = ServiceEnumerator.targets(services, overrides: ["Wi-Fi": false])
        #expect(excluded.isEmpty)
    }

    @Test func order缺失的服务仍保留() {
        let services = ServiceEnumerator.parse(listAll: listAllFixture, order: "")
        #expect(services.count == 3)
        #expect(services[1] == NetworkService(name: "Wi-Fi", hardwarePort: "", device: "", enabled: true))
    }
}