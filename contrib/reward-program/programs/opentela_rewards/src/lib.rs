use anchor_lang::prelude::*;
use anchor_lang::solana_program::{
    ed25519_program,
    instruction::Instruction,
    sysvar::instructions::{load_current_index_checked, load_instruction_at_checked},
};
use anchor_spl::{
    associated_token::AssociatedToken,
    token_interface::{self, Mint, TokenAccount, TokenInterface, TransferChecked},
};

declare_id!("HMSLAjUbu7XkMmw74fmTMoksg2TSaatDYZ8VjQUU4HdE");

const RECEIPT_DOMAIN: &[u8] = b"OPENTELA_REWARD_V1";
const ED25519_PUBKEY_LEN: usize = 32;
const ED25519_SIGNATURE_LEN: usize = 64;
const ED25519_HEADER_LEN: usize = 2;
const ED25519_OFFSET_LEN: usize = 14;
const ED25519_CURRENT_INSTRUCTION: u16 = u16::MAX;

#[program]
pub mod opentela_rewards {
    use super::*;

    pub fn initialize_campaign(
        ctx: Context<InitializeCampaign>,
        reward_per_unit: u64,
        head_authority: Pubkey,
    ) -> Result<()> {
        require!(reward_per_unit > 0, RewardError::InvalidRewardRate);

        let campaign = &mut ctx.accounts.campaign;
        campaign.authority = ctx.accounts.authority.key();
        campaign.head_authority = head_authority;
        campaign.reward_mint = ctx.accounts.reward_mint.key();
        campaign.vault = ctx.accounts.vault.key();
        campaign.vault_authority = ctx.accounts.vault_authority.key();
        campaign.reward_per_unit = reward_per_unit;
        campaign.paused = false;
        campaign.campaign_bump = ctx.bumps.campaign;
        campaign.vault_authority_bump = ctx.bumps.vault_authority;
        campaign.total_claims = 0;
        campaign.total_paid = 0;
        campaign.created_at = Clock::get()?.unix_timestamp;

        emit!(CampaignInitialized {
            campaign: campaign.key(),
            authority: campaign.authority,
            head_authority,
            reward_mint: campaign.reward_mint,
            vault: campaign.vault,
            reward_per_unit,
        });

        Ok(())
    }

    pub fn set_reward_rate(ctx: Context<UpdateCampaign>, reward_per_unit: u64) -> Result<()> {
        require!(reward_per_unit > 0, RewardError::InvalidRewardRate);

        let campaign = &mut ctx.accounts.campaign;
        campaign.reward_per_unit = reward_per_unit;

        emit!(RewardRateUpdated {
            campaign: campaign.key(),
            reward_per_unit,
        });

        Ok(())
    }

    pub fn set_head_authority(ctx: Context<UpdateCampaign>, head_authority: Pubkey) -> Result<()> {
        let campaign = &mut ctx.accounts.campaign;
        campaign.head_authority = head_authority;

        emit!(HeadAuthorityUpdated {
            campaign: campaign.key(),
            head_authority,
        });

        Ok(())
    }

    pub fn set_paused(ctx: Context<UpdateCampaign>, paused: bool) -> Result<()> {
        let campaign = &mut ctx.accounts.campaign;
        campaign.paused = paused;

        emit!(CampaignPauseUpdated {
            campaign: campaign.key(),
            paused,
        });

        Ok(())
    }

    pub fn claim_reward(
        ctx: Context<ClaimReward>,
        request_id: [u8; 32],
        service_hash: [u8; 32],
        units: u64,
        reward_per_unit: u64,
    ) -> Result<()> {
        let campaign = &mut ctx.accounts.campaign;
        require!(!campaign.paused, RewardError::CampaignPaused);
        require!(units > 0, RewardError::InvalidUsageUnits);
        require!(
            reward_per_unit == campaign.reward_per_unit,
            RewardError::RewardRateMismatch
        );

        let amount = units
            .checked_mul(reward_per_unit)
            .ok_or(RewardError::RewardOverflow)?;

        let message = receipt_message(
            &campaign.key(),
            &request_id,
            &ctx.accounts.provider.key(),
            &service_hash,
            units,
            reward_per_unit,
        );
        verify_previous_ed25519_instruction(
            &ctx.accounts.instructions.to_account_info(),
            &campaign.head_authority,
            &message,
        )?;

        transfer_from_vault(
            campaign,
            ctx.accounts.vault.to_account_info(),
            ctx.accounts.reward_mint.to_account_info(),
            ctx.accounts.provider_token_account.to_account_info(),
            ctx.accounts.vault_authority.to_account_info(),
            ctx.accounts.token_program.to_account_info(),
            amount,
            ctx.accounts.reward_mint.decimals,
        )?;

        let claim = &mut ctx.accounts.claim;
        claim.campaign = campaign.key();
        claim.provider = ctx.accounts.provider.key();
        claim.head_authority = campaign.head_authority;
        claim.request_id = request_id;
        claim.service_hash = service_hash;
        claim.units = units;
        claim.reward_per_unit = reward_per_unit;
        claim.amount = amount;
        claim.claimed_at = Clock::get()?.unix_timestamp;
        claim.bump = ctx.bumps.claim;

        campaign.total_claims = campaign
            .total_claims
            .checked_add(1)
            .ok_or(RewardError::CounterOverflow)?;
        campaign.total_paid = campaign
            .total_paid
            .checked_add(amount)
            .ok_or(RewardError::CounterOverflow)?;

        emit!(RewardClaimed {
            campaign: campaign.key(),
            request_id,
            provider: claim.provider,
            head_authority: campaign.head_authority,
            service_hash,
            units,
            reward_per_unit,
            amount,
        });

        Ok(())
    }

    pub fn withdraw_unclaimed(ctx: Context<WithdrawUnclaimed>, amount: u64) -> Result<()> {
        require!(amount > 0, RewardError::InvalidWithdrawAmount);

        transfer_from_vault(
            &ctx.accounts.campaign,
            ctx.accounts.vault.to_account_info(),
            ctx.accounts.reward_mint.to_account_info(),
            ctx.accounts.authority_token_account.to_account_info(),
            ctx.accounts.vault_authority.to_account_info(),
            ctx.accounts.token_program.to_account_info(),
            amount,
            ctx.accounts.reward_mint.decimals,
        )
    }
}

#[derive(Accounts)]
pub struct InitializeCampaign<'info> {
    #[account(mut)]
    pub authority: Signer<'info>,
    pub reward_mint: InterfaceAccount<'info, Mint>,
    #[account(
        init,
        payer = authority,
        space = RewardCampaign::SPACE,
        seeds = [b"campaign", authority.key().as_ref(), reward_mint.key().as_ref()],
        bump
    )]
    pub campaign: Account<'info, RewardCampaign>,
    /// CHECK: PDA authority for the reward vault. It never stores data.
    #[account(
        seeds = [b"vault_authority", campaign.key().as_ref()],
        bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    #[account(
        init,
        payer = authority,
        associated_token::mint = reward_mint,
        associated_token::authority = vault_authority,
        associated_token::token_program = token_program,
    )]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    pub token_program: Interface<'info, TokenInterface>,
    pub associated_token_program: Program<'info, AssociatedToken>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct UpdateCampaign<'info> {
    pub authority: Signer<'info>,
    #[account(mut, has_one = authority)]
    pub campaign: Account<'info, RewardCampaign>,
}

#[derive(Accounts)]
#[instruction(request_id: [u8; 32])]
pub struct ClaimReward<'info> {
    #[account(
        mut,
        has_one = reward_mint,
        has_one = vault,
        has_one = vault_authority,
    )]
    pub campaign: Account<'info, RewardCampaign>,
    #[account(mut)]
    pub provider: Signer<'info>,
    #[account(
        init,
        payer = provider,
        space = ClaimReceipt::SPACE,
        seeds = [b"claim", campaign.key().as_ref(), request_id.as_ref()],
        bump
    )]
    pub claim: Account<'info, ClaimReceipt>,
    #[account(
        mut,
        constraint = vault.mint == reward_mint.key() @ RewardError::InvalidVault,
        constraint = vault.owner == vault_authority.key() @ RewardError::InvalidVault,
    )]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA authority for the reward vault. It never stores data.
    #[account(
        seeds = [b"vault_authority", campaign.key().as_ref()],
        bump = campaign.vault_authority_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    pub reward_mint: InterfaceAccount<'info, Mint>,
    #[account(
        init_if_needed,
        payer = provider,
        associated_token::mint = reward_mint,
        associated_token::authority = provider,
        associated_token::token_program = token_program,
    )]
    pub provider_token_account: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: Solana instructions sysvar, constrained by address.
    #[account(address = anchor_lang::solana_program::sysvar::instructions::ID)]
    pub instructions: UncheckedAccount<'info>,
    pub token_program: Interface<'info, TokenInterface>,
    pub associated_token_program: Program<'info, AssociatedToken>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct WithdrawUnclaimed<'info> {
    #[account(mut)]
    pub authority: Signer<'info>,
    #[account(
        has_one = authority,
        has_one = reward_mint,
        has_one = vault,
        has_one = vault_authority,
    )]
    pub campaign: Account<'info, RewardCampaign>,
    #[account(
        mut,
        constraint = vault.mint == reward_mint.key() @ RewardError::InvalidVault,
        constraint = vault.owner == vault_authority.key() @ RewardError::InvalidVault,
    )]
    pub vault: InterfaceAccount<'info, TokenAccount>,
    /// CHECK: PDA authority for the reward vault. It never stores data.
    #[account(
        seeds = [b"vault_authority", campaign.key().as_ref()],
        bump = campaign.vault_authority_bump
    )]
    pub vault_authority: UncheckedAccount<'info>,
    pub reward_mint: InterfaceAccount<'info, Mint>,
    #[account(
        init_if_needed,
        payer = authority,
        associated_token::mint = reward_mint,
        associated_token::authority = authority,
        associated_token::token_program = token_program,
    )]
    pub authority_token_account: InterfaceAccount<'info, TokenAccount>,
    pub token_program: Interface<'info, TokenInterface>,
    pub associated_token_program: Program<'info, AssociatedToken>,
    pub system_program: Program<'info, System>,
}

#[account]
pub struct RewardCampaign {
    pub authority: Pubkey,
    pub head_authority: Pubkey,
    pub reward_mint: Pubkey,
    pub vault: Pubkey,
    pub vault_authority: Pubkey,
    pub reward_per_unit: u64,
    pub paused: bool,
    pub campaign_bump: u8,
    pub vault_authority_bump: u8,
    pub total_claims: u64,
    pub total_paid: u64,
    pub created_at: i64,
}

impl RewardCampaign {
    pub const SPACE: usize = 8 + 32 + 32 + 32 + 32 + 32 + 8 + 1 + 1 + 1 + 8 + 8 + 8;
}

#[account]
pub struct ClaimReceipt {
    pub campaign: Pubkey,
    pub provider: Pubkey,
    pub head_authority: Pubkey,
    pub request_id: [u8; 32],
    pub service_hash: [u8; 32],
    pub units: u64,
    pub reward_per_unit: u64,
    pub amount: u64,
    pub claimed_at: i64,
    pub bump: u8,
}

impl ClaimReceipt {
    pub const SPACE: usize = 8 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 8 + 8 + 1;
}

#[event]
pub struct CampaignInitialized {
    pub campaign: Pubkey,
    pub authority: Pubkey,
    pub head_authority: Pubkey,
    pub reward_mint: Pubkey,
    pub vault: Pubkey,
    pub reward_per_unit: u64,
}

#[event]
pub struct RewardRateUpdated {
    pub campaign: Pubkey,
    pub reward_per_unit: u64,
}

#[event]
pub struct HeadAuthorityUpdated {
    pub campaign: Pubkey,
    pub head_authority: Pubkey,
}

#[event]
pub struct CampaignPauseUpdated {
    pub campaign: Pubkey,
    pub paused: bool,
}

#[event]
pub struct RewardClaimed {
    pub campaign: Pubkey,
    pub request_id: [u8; 32],
    pub provider: Pubkey,
    pub head_authority: Pubkey,
    pub service_hash: [u8; 32],
    pub units: u64,
    pub reward_per_unit: u64,
    pub amount: u64,
}

#[error_code]
pub enum RewardError {
    #[msg("Reward rate must be greater than zero")]
    InvalidRewardRate,
    #[msg("Campaign is paused")]
    CampaignPaused,
    #[msg("Usage units must be greater than zero")]
    InvalidUsageUnits,
    #[msg("Claimed reward rate does not match the campaign rate")]
    RewardRateMismatch,
    #[msg("Reward amount overflow")]
    RewardOverflow,
    #[msg("Counter overflow")]
    CounterOverflow,
    #[msg("Invalid vault token account")]
    InvalidVault,
    #[msg("Invalid withdrawal amount")]
    InvalidWithdrawAmount,
    #[msg("Missing Ed25519 verification instruction")]
    MissingEd25519Instruction,
    #[msg("Invalid Ed25519 verification instruction")]
    InvalidEd25519Instruction,
    #[msg("Ed25519 verification instruction did not verify the expected receipt")]
    ReceiptSignatureMismatch,
}

pub fn receipt_message(
    campaign: &Pubkey,
    request_id: &[u8; 32],
    provider: &Pubkey,
    service_hash: &[u8; 32],
    units: u64,
    reward_per_unit: u64,
) -> Vec<u8> {
    let mut message = Vec::with_capacity(RECEIPT_DOMAIN.len() + 32 + 32 + 32 + 32 + 8 + 8);
    message.extend_from_slice(RECEIPT_DOMAIN);
    message.extend_from_slice(campaign.as_ref());
    message.extend_from_slice(request_id);
    message.extend_from_slice(provider.as_ref());
    message.extend_from_slice(service_hash);
    message.extend_from_slice(&units.to_le_bytes());
    message.extend_from_slice(&reward_per_unit.to_le_bytes());
    message
}

fn transfer_from_vault<'info>(
    campaign: &Account<'info, RewardCampaign>,
    vault: AccountInfo<'info>,
    mint: AccountInfo<'info>,
    destination: AccountInfo<'info>,
    vault_authority: AccountInfo<'info>,
    token_program: AccountInfo<'info>,
    amount: u64,
    decimals: u8,
) -> Result<()> {
    let campaign_key = campaign.key();
    let signer_seeds: &[&[&[u8]]] = &[&[
        b"vault_authority",
        campaign_key.as_ref(),
        &[campaign.vault_authority_bump],
    ]];

    let cpi_accounts = TransferChecked {
        from: vault,
        mint,
        to: destination,
        authority: vault_authority,
    };
    let cpi_context = CpiContext::new_with_signer(token_program, cpi_accounts, signer_seeds);
    token_interface::transfer_checked(cpi_context, amount, decimals)
}

fn verify_previous_ed25519_instruction(
    instructions: &AccountInfo,
    expected_pubkey: &Pubkey,
    expected_message: &[u8],
) -> Result<()> {
    let current_index = load_current_index_checked(instructions)?;
    require!(current_index > 0, RewardError::MissingEd25519Instruction);

    let ed25519_index = current_index - 1;
    let instruction = load_instruction_at_checked(ed25519_index as usize, instructions)?;
    verify_ed25519_instruction(&instruction, expected_pubkey, expected_message)
}

fn verify_ed25519_instruction(
    instruction: &Instruction,
    expected_pubkey: &Pubkey,
    expected_message: &[u8],
) -> Result<()> {
    require_keys_eq!(
        instruction.program_id,
        ed25519_program::ID,
        RewardError::InvalidEd25519Instruction
    );
    require!(
        instruction.data.len() >= ED25519_HEADER_LEN + ED25519_OFFSET_LEN,
        RewardError::InvalidEd25519Instruction
    );

    let signature_count = instruction.data[0] as usize;
    require!(signature_count > 0, RewardError::InvalidEd25519Instruction);

    for signature_index in 0..signature_count {
        let offset = ED25519_HEADER_LEN + signature_index * ED25519_OFFSET_LEN;
        if offset + ED25519_OFFSET_LEN > instruction.data.len() {
            return err!(RewardError::InvalidEd25519Instruction);
        }

        let signature_offset = read_u16(&instruction.data, offset)?;
        let signature_instruction_index = read_u16(&instruction.data, offset + 2)?;
        let public_key_offset = read_u16(&instruction.data, offset + 4)?;
        let public_key_instruction_index = read_u16(&instruction.data, offset + 6)?;
        let message_offset = read_u16(&instruction.data, offset + 8)?;
        let message_size = read_u16(&instruction.data, offset + 10)?;
        let message_instruction_index = read_u16(&instruction.data, offset + 12)?;

        if signature_instruction_index != ED25519_CURRENT_INSTRUCTION
            || public_key_instruction_index != ED25519_CURRENT_INSTRUCTION
            || message_instruction_index != ED25519_CURRENT_INSTRUCTION
        {
            continue;
        }

        let signature_start = signature_offset as usize;
        let public_key_start = public_key_offset as usize;
        let message_start = message_offset as usize;
        let message_end = message_start
            .checked_add(message_size as usize)
            .ok_or(RewardError::InvalidEd25519Instruction)?;

        if signature_start + ED25519_SIGNATURE_LEN > instruction.data.len()
            || public_key_start + ED25519_PUBKEY_LEN > instruction.data.len()
            || message_end > instruction.data.len()
        {
            return err!(RewardError::InvalidEd25519Instruction);
        }

        let verified_pubkey =
            &instruction.data[public_key_start..public_key_start + ED25519_PUBKEY_LEN];
        let verified_message = &instruction.data[message_start..message_end];

        if verified_pubkey == expected_pubkey.as_ref() && verified_message == expected_message {
            return Ok(());
        }
    }

    err!(RewardError::ReceiptSignatureMismatch)
}

fn read_u16(data: &[u8], offset: usize) -> Result<u16> {
    let bytes = data
        .get(offset..offset + 2)
        .ok_or(RewardError::InvalidEd25519Instruction)?;
    Ok(u16::from_le_bytes([bytes[0], bytes[1]]))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn receipt_message_is_deterministic() {
        let campaign = Pubkey::new_unique();
        let provider = Pubkey::new_unique();
        let request_id = [1u8; 32];
        let service_hash = [2u8; 32];

        let a = receipt_message(&campaign, &request_id, &provider, &service_hash, 123, 9);
        let b = receipt_message(&campaign, &request_id, &provider, &service_hash, 123, 9);
        let c = receipt_message(&campaign, &request_id, &provider, &service_hash, 124, 9);

        assert_eq!(a, b);
        assert_ne!(a, c);
        assert!(a.starts_with(RECEIPT_DOMAIN));
        assert_eq!(a.len(), RECEIPT_DOMAIN.len() + 32 + 32 + 32 + 32 + 8 + 8);
    }

    #[test]
    fn verifies_matching_ed25519_instruction_payload() {
        let head = Pubkey::new_unique();
        let message = b"receipt";
        let mut data = vec![0u8; ED25519_HEADER_LEN + ED25519_OFFSET_LEN];
        data[0] = 1;

        let public_key_offset = data.len() as u16;
        data.extend_from_slice(head.as_ref());
        let signature_offset = data.len() as u16;
        data.extend_from_slice(&[7u8; ED25519_SIGNATURE_LEN]);
        let message_offset = data.len() as u16;
        data.extend_from_slice(message);

        write_u16_for_test(&mut data, ED25519_HEADER_LEN, signature_offset);
        write_u16_for_test(
            &mut data,
            ED25519_HEADER_LEN + 2,
            ED25519_CURRENT_INSTRUCTION,
        );
        write_u16_for_test(&mut data, ED25519_HEADER_LEN + 4, public_key_offset);
        write_u16_for_test(
            &mut data,
            ED25519_HEADER_LEN + 6,
            ED25519_CURRENT_INSTRUCTION,
        );
        write_u16_for_test(&mut data, ED25519_HEADER_LEN + 8, message_offset);
        write_u16_for_test(&mut data, ED25519_HEADER_LEN + 10, message.len() as u16);
        write_u16_for_test(
            &mut data,
            ED25519_HEADER_LEN + 12,
            ED25519_CURRENT_INSTRUCTION,
        );

        let instruction = Instruction {
            program_id: ed25519_program::ID,
            accounts: vec![],
            data,
        };

        verify_ed25519_instruction(&instruction, &head, message).unwrap();
        assert!(verify_ed25519_instruction(&instruction, &Pubkey::new_unique(), message).is_err());
        assert!(verify_ed25519_instruction(&instruction, &head, b"other").is_err());
    }

    fn write_u16_for_test(data: &mut [u8], offset: usize, value: u16) {
        data[offset..offset + 2].copy_from_slice(&value.to_le_bytes());
    }
}
