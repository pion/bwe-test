from mininet.topo import Topo


class SingleSwitchTopo(Topo):
    def build(self, bw, delay, loss):
        switch = self.addSwitch('s1')
        h1 = self.addHost('h1')
        h2 = self.addHost('h2')
        self.addLink(h1, switch, bw=bw, delay=delay, loss=loss,
                     latency_ms=300, use_tbf=True)
        self.addLink(h2, switch, bw=bw, delay=delay, loss=loss,
                     latency_ms=300, use_tbf=True)


class DumbbellTopo(Topo):
    def build(self, n=2):
        left_switch = self.addSwitch('ls1')
        right_switch = self.addSwitch('rs1')
        self.addLink(left_switch, right_switch)

        for h in range(n):
            left_host = self.addHost('l{}'.format(h), cpu=.5 / n)
            self.addLink(left_host, left_switch)
            right_host = self.addHost('r{}'.format(h), cpu=.5 / n)
            self.addLink(right_host, right_switch)
