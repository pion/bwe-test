# PR #158 — Analysis of JoTurk's "FrameReady stall" review comment

- **PR:** https://github.com/pion/bwe-test/pull/158 — *Add poll-free single-slot encode loop*
- **Branch:** `lkang/single-slot-buffer`
- **Review comment:** JoTurk on `sender/rtc_sender.go:828`
  > Can this consume the only FrameReady signal even when the queued frame was not
  > read (ErrNoFrameAvailable), causing the sender to stall until another frame
  > arrives? I don't see a test to confirm if this doesn't happen, and i think
  > this can happen.

**Verdict:** JoTurk's literal observation is correct (a signal *can* be consumed on a
no-frame iteration), but it **cannot cause a stall**. The only real gap is the
missing regression test he calls out.

> Note: the review the PR link points at (`#pullrequestreview-4527367394`, by
> hanguyen-nuro) is an **APPROVED** review with an empty body — no concern there.
> The substantive comment is JoTurk's separate `COMMENTED` review.

---

## Two facts the concern hinges on

**Fact 1 — the frame and the wakeup signal are read on *separate* paths.**

- The frame lives in `frameChan` (cap 1). Popped only by `FrameBuffer.Read()`
  (`frame_buffer.go:183`).
- The wakeup lives in `notifyChan` (cap 1), exposed as `FrameReady()`. Consumed only
  in `handleEncodeErr` (`rtc_sender.go:821`).

These two channels are **not drained together**. When a frame is read on the fast
path (main loop, `rtc_sender.go:787`), the consumer takes the frame out of
`frameChan` but **leaves the matching signal sitting in `notifyChan`**.

**Fact 2 — the producer pushes the frame, then signals** (`frame_buffer.go:249-250`):

```go
case f.frameChan <- fm:
    f.signal()        // notifyChan <- {} , coalesced if one already pending
```

---

## What JoTurk is saying

His comment targets the `handleEncodeErr` body, specifically `rtc_sender.go:821`:

```go
case <-frameReady:   // consumes one signal from notifyChan
    return false
```

Worry: this `case` fires — consuming the one signal in `notifyChan` — on an iteration
where `Read()` returned `ErrNoFrameAvailable` (**no frame actually taken**). If that's
the only signal, and a frame is/was sitting in the buffer, the loop has "spent" its
wakeup without consuming the frame, so the next `Read()` finds nothing and blocks
forever → **stall until another frame arrives**.

---

## Example — walk the interleaving he fears

Two goroutines: **P** = producer (`SendFrame`), **C** = consumer (encode loop).
Start state: `frameChan []`, `notifyChan []`.

```
t1  P: SendFrame(A)
       frame_buffer.go:249  frameChan <- A      -> frameChan [A]
       frame_buffer.go:250  signal()            -> notifyChan [sig]   (A queued, 1 sig)

t2  C: iteration N, encodeAndSendTrack -> Read()  (rtc_sender.go:787 -> 847)
       frame_buffer.go:183  fm := <-frameChan   -> reads A, frameChan []
       -- notifyChan is NOT touched --            (frameChan [], notifyChan [sig])
       encodes A, hasFrame=true, loops

t3  C: iteration N+1 -> Read()
       frame_buffer.go:188  default -> ErrNoFrameAvailable
       rtc_sender.go:815  handleEncodeErr, err == ErrNoFrameAvailable
       rtc_sender.go:821  case <-frameReady:  <- FIRES, consumes the [sig]   (*)
                          returns false          (frameChan [], notifyChan [])
```

(*) **This is exactly what JoTurk describes:** at `t3` the loop consumed the only
`FrameReady` signal on an iteration where no frame was read. His observation is
literally correct — it does happen.

Does it stall? Continue:

```
t4  C: iteration N+2 -> Read()
       frame_buffer.go:188  default -> ErrNoFrameAvailable again
       rtc_sender.go:821  case <-frameReady:  <- BLOCKS (notifyChan empty)   correctly idle

t5  P: SendFrame(B)
       frame_buffer.go:249  frameChan <- B      -> frameChan [B]
       frame_buffer.go:250  signal()            -> notifyChan [sig]

t6  C: wakes at rtc_sender.go:821, returns false, loops -> Read() reads B   OK
```

**No stall.** The signal consumed at `t3` was a *stale leftover* from frame A — a
frame the consumer had **already read** at `t2`. Spending it cost one extra spin
(`t3`->`t4`). At `t4` the loop is correctly blocked because there genuinely is no
frame, and the next real frame B re-arms `notifyChan` and wakes it.

---

## Why it can never actually stall

A real stall needs this end state: **C blocked on `notifyChan` (empty) while
`frameChan` holds an unread frame.** That state is unreachable, because of the
safety invariant:

> Every push to `frameChan` posts a signal (or coalesces onto a pending one), the
> frame **stays in `frameChan` until `Read()` pops it**, and the consumer **always
> calls `Read()` again after consuming a signal** (`return false` -> loop ->
> `encodeAndSendTrack`).

So any frame present in `frameChan` is guaranteed to be seen by the next `Read()`
after the next wake. A "spent" signal can only ever correspond to a frame that was
*already read* — never one still waiting. Worst case is a wasted spin, not a lost
frame.

The race that *would* break this — a frame pushed in the gap between `Read()`
returning `ErrNoFrameAvailable` (`t3`/`t4`) and the consumer reaching the `select` —
is closed because `notifyChan` is **buffered** (cap 1): a signal sent in that window
is stored, so the consumer wakes immediately instead of missing it.

Supporting guarantees in `SendFrameWithCaptureTS` (`frame_buffer.go:239-271`):

- A signal is **never** posted without a frame: `signal()` is called only inside the
  two successful `frameChan <- fm` branches (lines 250, 264).
- A frame is **never** pushed without a signal: both success branches signal; the only
  no-signal exit is `ErrFailedToAddFrameAfterDrop` (line 268), which pushed nothing.
- The drop-oldest path keeps `frameChan` depth at 1 and coalesces signals at cap 1; no
  interleaving with a concurrent `Read()` leaves a frame stranded with no signal.

---

## Bottom line / recommended response

JoTurk is right that the signal *can* be consumed on a no-frame iteration — but that
consumed signal always belongs to an already-read frame, so it costs at most one extra
non-blocking `Read()`, never a stall. The legitimate gap is the one he names at the
end: **there is no test pinning this invariant** (`frame_buffer_test.go:195`
`TestFrameBuffer_ConcurrentAccess` exercises concurrent push/read but not the
block-then-wake liveness path through `FrameReady()`).

**Action:** add a regression test that forces the `t2 -> t3 -> t4` stale-signal
sequence and asserts the next frame is still delivered:

1. Start the encode loop with an empty buffer (confirm it blocks, not spins).
2. Push one frame via `SendFrame`.
3. Assert the frame gets encoded/sent — proving the block -> wake path delivers even
   after a stale signal was spent.
