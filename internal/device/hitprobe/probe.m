// The hit test below accessibility: which view UIKit would give a touch.
//
// mobium compiles this against the simulator SDK, loads it into the app in
// front through lldb, calls MOBIUM_HIT once, and detaches. Nothing stays
// behind but the loaded library, which does nothing unless called.
//
// WebDriverAgent's tree and XCTest's `hittable` are both accessibility, which
// leaves out a view hidden from it: a tap under an overlay hidden from
// accessibility landed on the overlay while every check passed (CHALLENGES
// 115). UIKit's own hitTest:withEvent: is what decides where a touch goes,
// and it knows nothing of accessibility.
//
// MOBIUM_HIT is defined on the command line, a name unique to this source,
// so a library loaded by an older mobium is never the one called.
#import <UIKit/UIKit.h>

static UIWindow *keyWindow(void) {
    for (UIScene *scene in UIApplication.sharedApplication.connectedScenes) {
        if (![scene isKindOfClass:UIWindowScene.class]) continue;
        for (UIWindow *w in ((UIWindowScene *)scene).windows) {
            if (w.isKeyWindow) return w;
        }
    }
    return nil;
}

static BOOL sameFrame(CGRect a, CGRect b) {
    return fabs(a.origin.x - b.origin.x) <= 1 && fabs(a.origin.y - b.origin.y) <= 1 &&
           fabs(a.size.width - b.size.width) <= 1 && fabs(a.size.height - b.size.height) <= 1;
}

// The element's own views: by accessibility identifier when it has one,
// since a covered element's frame is not always the one accessibility
// reports; otherwise the accessibility elements at its frame.
static void elementViews(UIView *v, CGRect frame, NSString *ident, NSMutableArray *out) {
    BOOL match = ident.length > 0 ? [v.accessibilityIdentifier isEqualToString:ident]
                                  : (v.isAccessibilityElement && sameFrame(v.accessibilityFrame, frame));
    if (match) [out addObject:v];
    for (UIView *c in v.subviews) elementViews(c, frame, ident, out);
}

static NSString *clean(NSString *s) {
    s = [s stringByReplacingOccurrencesOfString:@"\t" withString:@" "];
    return [s stringByReplacingOccurrencesOfString:@"\n" withString:@" "] ?: @"";
}

// MOBIUM_HIT answers, for a touch at (x, y) in points, one line of tab-
// separated fields:
//   reaches
//   covered <hidden 0|1> <class> <label> <x> <y> <w> <h>
//   nothing                       (no view takes a touch there)
//   unknown <reason>
const char *MOBIUM_HIT(double x, double y, double fx, double fy, double fw, double fh, const char *cid) {
    __block NSString *answer = @"unknown\tthe app has no key window";
    void (^probe)(void) = ^{
        UIWindow *w = keyWindow();
        if (!w) return;
        NSMutableArray *views = [NSMutableArray array];
        CGRect frame = CGRectMake(fx, fy, fw, fh);
        elementViews(w, frame, [NSString stringWithUTF8String:cid], views);
        // WebDriverAgent names an element with no identifier by its label,
        // which no view carries as an identifier: then by frame.
        if (views.count == 0) elementViews(w, frame, @"", views);
        if (views.count == 0) {
            answer = @"unknown\tno view in the app is that element";
            return;
        }
        UIView *hit = [w hitTest:CGPointMake(x, y) withEvent:nil];
        if (!hit) {
            answer = @"nothing";
            return;
        }
        for (UIView *v in views) {
            if ([hit isDescendantOfView:v]) {
                answer = @"reaches";
                return;
            }
        }
        BOOL hidden = NO;
        NSString *label = @"";
        for (UIView *a = hit; a; a = a.superview) {
            if (a.accessibilityElementsHidden) hidden = YES;
            if (label.length == 0) label = a.accessibilityLabel ?: @"";
        }
        CGRect r = [hit convertRect:hit.bounds toView:nil];
        answer = [NSString stringWithFormat:@"covered\t%d\t%@\t%@\t%.1f\t%.1f\t%.1f\t%.1f", (int)hidden,
            NSStringFromClass(hit.class), clean(label), r.origin.x, r.origin.y, r.size.width, r.size.height];
    };
    if (NSThread.isMainThread) probe();
    else dispatch_sync(dispatch_get_main_queue(), probe);
    return strdup(answer.UTF8String);
}
