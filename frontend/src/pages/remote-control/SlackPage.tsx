import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const SlackPage = () => {
    const config = usePlatformGuide('slack');

    return (
        <PlatformBotPage
            platformId="slack"
            platformName={config?.name || 'Slack'}
            platformGuide={config?.guide}
        />
    );
};

export default SlackPage;
