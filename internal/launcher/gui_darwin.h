#include <stdint.h>

typedef enum { TTStopped, TTStarting, TTRunning, TTStopping } TTState;
typedef enum { TTPreview, TTWrite, TTCopy } TTMode;

void ttRun(void);
void ttPost(uintptr_t handle);
void ttUpdate(int state, const char *url, const char *message);
int ttOpenURL(const char *url);
void ttFinishQuit(void);
