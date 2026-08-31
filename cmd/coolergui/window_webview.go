//go:build cgo && darwin

// SPDX-License-Identifier: AGPL-3.0-or-later

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

@interface CoolerWindowDelegate : NSObject <NSWindowDelegate, WKNavigationDelegate>
@end

@implementation CoolerWindowDelegate
- (void)windowWillClose:(NSNotification *)notification {
    [NSApp terminate:nil];
}

- (void)webView:(WKWebView *)webView
        decidePolicyForNavigationAction:(WKNavigationAction *)navigationAction
        decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    NSURL *url = navigationAction.request.URL;
    if (navigationAction.targetFrame == nil && url != nil) {
        [[NSWorkspace sharedWorkspace] openURL:url];
        decisionHandler(WKNavigationActionPolicyCancel);
        return;
    }
    decisionHandler(WKNavigationActionPolicyAllow);
}
@end

static CoolerWindowDelegate *coolerDelegate;
static NSWindow *coolerWindow;
static WKWebView *coolerWebView;

static void installCoolerMainMenu(NSApplication *app) {
    NSMenu *mainMenu = [[NSMenu alloc] initWithTitle:@""];
    NSMenuItem *appMenuItem = [[NSMenuItem alloc] initWithTitle:@"Cooler Compiler Lab"
                                                        action:nil
                                                 keyEquivalent:@""];
    NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"Cooler Compiler Lab"];
    NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"Quit Cooler Compiler Lab"
                                                      action:@selector(terminate:)
                                               keyEquivalent:@"q"];
    [quitItem setKeyEquivalentModifierMask:NSEventModifierFlagCommand];
    [appMenu addItem:quitItem];
    [appMenuItem setSubmenu:appMenu];
    [mainMenu addItem:appMenuItem];
    [app setMainMenu:mainMenu];
}

static void runCoolerWebView(const char *rawURL, int debug) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyRegular];
        installCoolerMainMenu(app);

        NSRect frame = NSMakeRect(0, 0, 1360, 860);
        NSScreen *screen = [NSScreen mainScreen];
        if (screen != nil) {
            frame = screen.visibleFrame;
        }

        NSWindowStyleMask style = NSWindowStyleMaskTitled |
                                  NSWindowStyleMaskClosable |
                                  NSWindowStyleMaskMiniaturizable |
                                  NSWindowStyleMaskResizable;
        coolerWindow = [[NSWindow alloc] initWithContentRect:frame
                                                   styleMask:style
                                                     backing:NSBackingStoreBuffered
                                                       defer:NO];
        [coolerWindow setTitle:@"Cooler Compiler Lab"];
        [coolerWindow setMinSize:NSMakeSize(980, 640)];
        [coolerWindow center];

        WKWebViewConfiguration *configuration = [[WKWebViewConfiguration alloc] init];
        if (debug) {
            [configuration.preferences setValue:@YES forKey:@"developerExtrasEnabled"];
        }
        coolerWebView = [[WKWebView alloc] initWithFrame:coolerWindow.contentView.bounds
                                           configuration:configuration];
        [coolerWebView setAutoresizingMask:NSViewWidthSizable | NSViewHeightSizable];
        [coolerWindow.contentView addSubview:coolerWebView];

        coolerDelegate = [[CoolerWindowDelegate alloc] init];
        [coolerWindow setDelegate:coolerDelegate];
        [coolerWebView setNavigationDelegate:coolerDelegate];

        NSString *urlString = [NSString stringWithUTF8String:rawURL];
        NSURL *url = [NSURL URLWithString:urlString];
        [coolerWebView loadRequest:[NSURLRequest requestWithURL:url]];

        [coolerWindow makeKeyAndOrderFront:nil];
        [app activateIgnoringOtherApps:YES];
        [app run];
    }
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

func runNativeWindow(url string, debug bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	debugFlag := C.int(0)
	if debug {
		debugFlag = 1
	}
	C.runCoolerWebView(cURL, debugFlag)
	return nil
}
