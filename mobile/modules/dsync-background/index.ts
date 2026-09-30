// Android only (see android/). On iOS, and in Expo Go, this is null.
import { requireOptionalNativeModule } from 'expo';

export interface DsyncBackgroundModule {
  /** Shows or refreshes the ongoing transfer notification; progress 0..100 or -1. */
  update(title: string, text: string, progress: number, upload: boolean): boolean;
  stop(): void;
}

export default requireOptionalNativeModule<DsyncBackgroundModule>('DsyncBackground');
