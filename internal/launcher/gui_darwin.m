//go:build cgo

#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

@interface TTLauncher : NSObject <NSApplicationDelegate, NSWindowDelegate>
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, strong) NSTextField *source;
@property(nonatomic, strong) NSTextField *destination;
@property(nonatomic, strong) NSPopUpButton *mode;
@property(nonatomic, strong) NSButton *chooseSource;
@property(nonatomic, strong) NSButton *chooseDestination;
@property(nonatomic, strong) NSButton *start;
@property(nonatomic, strong) NSButton *stop;
@property(nonatomic, strong) NSButton *review;
@property(nonatomic, strong) NSTextField *status;
@property(nonatomic, strong) NSTextField *details;
@property(nonatomic, strong) NSTextField *reviewURL;
- (void)showState:(TTState)state url:(NSString *)url message:(NSString *)message;
@end

static TTLauncher *launcher;

@implementation TTLauncher

- (NSButton *)button:(NSString *)title action:(SEL)action {
    return [NSButton buttonWithTitle:title target:self action:action];
}

- (NSTextField *)folderField:(NSString *)label key:(NSString *)key {
    NSTextField *field = [NSTextField textFieldWithString:[NSUserDefaults.standardUserDefaults stringForKey:key] ?: @""];
    field.editable = NO;
    field.selectable = YES;
    field.lineBreakMode = NSLineBreakByTruncatingMiddle;
    field.placeholderString = @"Choose a folder";
    field.toolTip = field.stringValue;
    [field setAccessibilityLabel:label];
    return field;
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    self.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 660, 267)
                                             styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable
                                               backing:NSBackingStoreBuffered
                                                 defer:NO];
    self.window.title = @"Track Tagger";
    self.window.delegate = self;
    self.window.restorable = NO;

    self.source = [self folderField:@"Music folder" key:@"Source"];
    self.destination = [self folderField:@"Copy destination" key:@"Destination"];
    self.chooseSource = [self button:@"Choose…" action:@selector(chooseSourceFolder:)];
    self.chooseDestination = [self button:@"Choose…" action:@selector(chooseDestinationFolder:)];
    [self.chooseSource setAccessibilityLabel:@"Choose music folder"];
    [self.chooseDestination setAccessibilityLabel:@"Choose copy destination"];
    self.mode = [[NSPopUpButton alloc] initWithFrame:NSZeroRect pullsDown:NO];
    [self.mode addItemsWithTitles:@[@"Preview", @"Write", @"Copy"]];
    [self.mode selectItemAtIndex:TTPreview];
    [self.mode setAccessibilityLabel:@"Mode"];
    self.mode.target = self;
    self.mode.action = @selector(modeChanged:);
    NSString *mode = [NSUserDefaults.standardUserDefaults stringForKey:@"Mode"];
    if (mode != nil && [self.mode indexOfItemWithTitle:mode] >= 0) {
        [self.mode selectItemWithTitle:mode];
    }

    NSGridView *folders = [NSGridView gridViewWithViews:@[
        @[[NSTextField labelWithString:@"Music folder"], self.source, self.chooseSource],
        @[[NSTextField labelWithString:@"Copy destination"], self.destination, self.chooseDestination],
        @[[NSTextField labelWithString:@"Mode"], self.mode, NSGridCell.emptyContentView],
    ]];
    folders.columnSpacing = 12;
    folders.rowSpacing = 10;
    [folders columnAtIndex:0].xPlacement = NSGridCellPlacementLeading;
    [folders cellAtColumnIndex:1 rowIndex:0].xPlacement = NSGridCellPlacementFill;
    [folders cellAtColumnIndex:1 rowIndex:1].xPlacement = NSGridCellPlacementFill;
    [folders cellAtColumnIndex:1 rowIndex:2].xPlacement = NSGridCellPlacementLeading;

    self.start = [self button:@"Start" action:@selector(startService:)];
    self.stop = [self button:@"Stop" action:@selector(stopService:)];
    self.review = [self button:@"Open review" action:@selector(openReview:)];
    self.start.keyEquivalent = @"\r";
    NSStackView *actions = [NSStackView stackViewWithViews:@[self.start, self.stop, self.review]];
    actions.spacing = 8;

    self.status = [NSTextField labelWithString:@""];
    self.status.font = [NSFont boldSystemFontOfSize:NSFont.systemFontSize];
    self.reviewURL = [NSTextField labelWithString:@""];
    self.reviewURL.selectable = YES;
    self.reviewURL.textColor = NSColor.secondaryLabelColor;
    NSStackView *state = [NSStackView stackViewWithViews:@[self.status, self.reviewURL]];
    state.spacing = 12;

    self.details = [NSTextField wrappingLabelWithString:@""];
    self.details.textColor = NSColor.secondaryLabelColor;
    self.details.selectable = YES;

    NSStackView *layout = [NSStackView stackViewWithViews:@[folders, actions, state, self.details]];
    layout.orientation = NSUserInterfaceLayoutOrientationVertical;
    layout.alignment = NSLayoutAttributeLeading;
    layout.spacing = 14;
    layout.translatesAutoresizingMaskIntoConstraints = NO;
    NSView *content = self.window.contentView;
    [content addSubview:layout];
    [NSLayoutConstraint activateConstraints:@[
        [layout.leadingAnchor constraintEqualToAnchor:content.leadingAnchor constant:20],
        [layout.trailingAnchor constraintEqualToAnchor:content.trailingAnchor constant:-20],
        [layout.topAnchor constraintEqualToAnchor:content.topAnchor constant:20],
        [folders.widthAnchor constraintEqualToAnchor:layout.widthAnchor],
        [self.details.widthAnchor constraintEqualToAnchor:layout.widthAnchor],
    ]];

    [self showState:TTStopped url:@"" message:@""];
    [self.window center];
    [self.window makeKeyAndOrderFront:nil];
    if (@available(macOS 14.0, *)) {
        [NSApp activate];
    } else {
        [NSApp activateIgnoringOtherApps:YES];
    }
}

- (void)showState:(TTState)state url:(NSString *)url message:(NSString *)message {
    NSArray *states = @[@"Stopped", @"Starting", @"Running", @"Stopping"];
    self.status.stringValue = [NSString stringWithFormat:@"%@ · %@", self.mode.titleOfSelectedItem, states[state]];
    self.reviewURL.stringValue = url;
    self.details.stringValue = message;
    self.details.toolTip = message;
    BOOL stopped = state == TTStopped;
    BOOL canChooseDestination = stopped && self.mode.indexOfSelectedItem == TTCopy;
    self.mode.enabled = stopped;
    self.source.enabled = stopped;
    self.chooseSource.enabled = stopped;
    self.destination.enabled = canChooseDestination;
    self.chooseDestination.enabled = canChooseDestination;
    self.start.enabled = stopped;
    self.stop.enabled = state == TTStarting || state == TTRunning;
    self.review.enabled = state == TTRunning;
}

- (void)modeChanged:(id)sender {
    [NSUserDefaults.standardUserDefaults setObject:self.mode.titleOfSelectedItem forKey:@"Mode"];
    [self showState:TTStopped url:@"" message:@""];
}

- (void)chooseFolderForField:(NSTextField *)field key:(NSString *)key title:(NSString *)title {
    NSOpenPanel *panel = [NSOpenPanel openPanel];
    panel.title = title;
    panel.prompt = @"Choose";
    panel.canChooseDirectories = YES;
    panel.canChooseFiles = NO;
    if (field.stringValue.length != 0) {
        panel.directoryURL = [NSURL fileURLWithPath:field.stringValue isDirectory:YES];
    }
    [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result) {
        if (result == NSModalResponseOK) {
            field.stringValue = panel.URL.path;
            field.toolTip = field.stringValue;
            [NSUserDefaults.standardUserDefaults setObject:field.stringValue forKey:key];
            [self showState:TTStopped url:@"" message:@""];
        }
    }];
}

- (void)chooseSourceFolder:(id)sender {
    [self chooseFolderForField:self.source key:@"Source" title:@"Choose music folder"];
}

- (void)chooseDestinationFolder:(id)sender {
    [self chooseFolderForField:self.destination key:@"Destination" title:@"Choose copy destination"];
}

- (void)startService:(id)sender {
    NSString *source = self.source.stringValue;
    NSString *destination = self.destination.stringValue;
    ttStart((char *)(source.length ? source.fileSystemRepresentation : ""),
            (char *)(destination.length ? destination.fileSystemRepresentation : ""),
            (int)self.mode.indexOfSelectedItem);
}

- (void)stopService:(id)sender { ttStop(); }
- (void)openReview:(id)sender { ttOpenReview(); }

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    return ttRequestQuit() ? NSTerminateLater : NSTerminateNow;
}

- (BOOL)windowShouldClose:(NSWindow *)sender {
    [NSApp terminate:nil];
    return NO;
}

@end

static void setMainMenu(void) {
    NSMenu *menu = [[NSMenu alloc] init];
    NSMenuItem *appItem = [[NSMenuItem alloc] init];
    appItem.submenu = [[NSMenu alloc] initWithTitle:@"Track Tagger"];
    [appItem.submenu addItemWithTitle:@"Quit Track Tagger" action:@selector(terminate:) keyEquivalent:@"q"];
    [menu addItem:appItem];
    NSMenuItem *editItem = [[NSMenuItem alloc] init];
    editItem.submenu = [[NSMenu alloc] initWithTitle:@"Edit"];
    [editItem.submenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
    [editItem.submenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    [menu addItem:editItem];
    NSApp.mainMenu = menu;
}

void ttRun(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        setMainMenu();
        launcher = [[TTLauncher alloc] init];
        NSApp.delegate = launcher;
        [NSApp run];
    }
}

void ttPost(uintptr_t handle) {
    @autoreleasepool {
        CFRunLoopRef loop = CFRunLoopGetMain();
        // AppKit waits in a modal run loop after NSTerminateLater.
        NSArray *modes = @[NSRunLoopCommonModes, NSModalPanelRunLoopMode];
        CFRunLoopPerformBlock(loop, (__bridge CFArrayRef)modes, ^{ ttDispatch(handle); });
        CFRunLoopWakeUp(loop);
    }
}

void ttUpdate(int state, const char *url, const char *message) {
    [launcher showState:state url:@(url) message:@(message)];
}

int ttOpenURL(const char *url) {
    return [NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:@(url)]];
}

void ttFinishQuit(void) {
    [NSApp replyToApplicationShouldTerminate:YES];
}
