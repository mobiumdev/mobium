# The hit test below accessibility, on a real iPhone: the question probe.m
# asks on a simulator, asked as one Objective-C expression, since a phone
# runs only code signed for it and the probe cannot be loaded as a library.
#
# mobium runs this inside `xcrun lldb`, whose own Python it is, as the
# command mobium_hit. lldb's device commands attach to an app on a phone
# through CoreDevice; batch mode then never sees the stop, so the attach is
# made on a debugger of its own, in asynchronous mode, and the stop waited
# for as an event. It always detaches, error or not: an lldb that quit while
# attached took the app down with it.
#
# It prints one line, MOBIUM_HIT and then probe.m's answer:
#   reaches
#   covered <hidden 0|1> <class> <label> <x> <y> <w> <h>
#   nothing
#   unknown <reason>

import shlex
import time

import lldb

EXPR = r'''
UIWindow *w = nil;
for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
  if (![scene isKindOfClass:[UIWindowScene class]]) continue;
  for (UIWindow *ww in ((UIWindowScene *)scene).windows) { if (ww.isKeyWindow) w = ww; }
}
NSString *answer = @"unknown\tthe app has no key window";
if (w) {
  CGRect frame = CGRectMake(%(fx)f, %(fy)f, %(fw)f, %(fh)f);
  NSMutableArray *views = [NSMutableArray array];
  for (int pass = 0; pass < 2 && views.count == 0; pass++) {
    NSString *ident = pass == 0 ? @"%(cid)s" : @"";
    NSMutableArray *stack = [NSMutableArray arrayWithObject:w];
    while (stack.count) {
      UIView *v = [stack lastObject]; [stack removeLastObject];
      CGRect af = v.accessibilityFrame;
      BOOL match = ident.length > 0 ? [v.accessibilityIdentifier isEqualToString:ident]
        : (v.isAccessibilityElement && fabs(af.origin.x - frame.origin.x) <= 1 && fabs(af.origin.y - frame.origin.y) <= 1
           && fabs(af.size.width - frame.size.width) <= 1 && fabs(af.size.height - frame.size.height) <= 1);
      if (match) [views addObject:v];
      [stack addObjectsFromArray:v.subviews];
    }
  }
  if (views.count == 0) { answer = @"unknown\tno view in the app is that element"; }
  else {
    UIView *hit = [w hitTest:CGPointMake(%(x)f, %(y)f) withEvent:nil];
    if (!hit) { answer = @"nothing"; }
    else {
      BOOL reaches = NO;
      for (UIView *v in views) { if ([hit isDescendantOfView:v]) reaches = YES; }
      if (reaches) { answer = @"reaches"; }
      else {
        BOOL hidden = NO; NSString *label = @"";
        for (UIView *a = hit; a; a = a.superview) {
          if (a.accessibilityElementsHidden) hidden = YES;
          if (label.length == 0 && a.accessibilityLabel) label = a.accessibilityLabel;
        }
        label = [[label stringByReplacingOccurrencesOfString:@"\t" withString:@" "]
                 stringByReplacingOccurrencesOfString:@"\n" withString:@" "];
        CGRect r = [hit convertRect:hit.bounds toView:nil];
        answer = [NSString stringWithFormat:@"covered\t%%d\t%%@\t%%@\t%%.1f\t%%.1f\t%%.1f\t%%.1f", (int)hidden,
          NSStringFromClass([hit class]), label, r.origin.x, r.origin.y, r.size.width, r.size.height];
      }
    }
  }
}
answer;
'''

ATTACH_WAIT = 20  # seconds; an attach on an iPhone 15 Plus took about five


def objc_string(s):
    return s.replace('\\', '\\\\').replace('"', '\\"')


def mobium_hit(debugger, command, result, internal_dict):
    args = shlex.split(command)
    udid, pid = args[0], int(args[1])
    x, y, fx, fy, fw, fh = (float(v) for v in args[2:8])
    cid = args[8] if len(args) > 8 else ''
    answer = 'unknown\tthe app did not stop for the debugger'
    dbg = lldb.SBDebugger.Create()
    dbg.SetAsync(True)
    ci = dbg.GetCommandInterpreter()

    def run(c):
        r = lldb.SBCommandReturnObject()
        ci.HandleCommand(c, r)
        return r

    run('settings set target.detach-on-error true')
    run('device select ' + udid)
    r = run('device process attach -p %d' % pid)
    proc = dbg.GetSelectedTarget().GetProcess()
    try:
        if not r.Succeeded() or not proc.IsValid():
            answer = 'unknown\tlldb could not attach: ' + (r.GetError() or 'no process').strip()
            return
        event, deadline = lldb.SBEvent(), time.time() + ATTACH_WAIT
        while time.time() < deadline:
            if (dbg.GetListener().WaitForEvent(1, event) and lldb.SBProcess.EventIsProcessEvent(event)
                    and lldb.SBProcess.GetStateFromEvent(event) == lldb.eStateStopped):
                break
        if proc.GetState() != lldb.eStateStopped:
            return
        # UIKit only on the main thread, which is the first.
        proc.SetSelectedThreadByIndexID(1)
        opts = lldb.SBExpressionOptions()
        opts.SetTimeoutInMicroSeconds(10000000)
        opts.SetLanguage(lldb.eLanguageTypeObjC)
        opts.SetTryAllThreads(False)
        target = dbg.GetSelectedTarget()
        target.EvaluateExpression('@import UIKit', opts)
        value = target.EvaluateExpression(EXPR % dict(x=x, y=y, fx=fx, fy=fy, fw=fw, fh=fh, cid=objc_string(cid)), opts)
        if value.GetError().Fail():
            answer = 'unknown\tthe expression failed: ' + (value.GetError().GetCString() or '').replace('\n', ' ')
        else:
            answer = value.GetObjectDescription() or 'unknown\tthe expression returned nothing'
    finally:
        if proc.IsValid():
            proc.Detach()
        lldb.SBDebugger.Destroy(dbg)
        print('MOBIUM_HIT\t' + answer.replace('\n', ' '))


def __lldb_init_module(debugger, internal_dict):
    debugger.HandleCommand('command script add -f %s.mobium_hit mobium_hit' % __name__)
