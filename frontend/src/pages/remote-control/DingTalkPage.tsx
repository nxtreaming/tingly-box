import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const DingTalkPage = () => {
    const config = usePlatformGuide('dingtalk');

    return (
        <PlatformBotPage
            platformId="dingtalk"
            platformName={config?.name || 'DingTalk (钉钉)'}
            platformGuide={config?.guide}
        />
    );
};

export default DingTalkPage;
