import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const DiscordPage = () => {
    const config = usePlatformGuide('discord');

    return (
        <PlatformBotPage
            platformId="discord"
            platformName={config?.name || 'Discord'}
            platformGuide={config?.guide}
        />
    );
};

export default DiscordPage;
